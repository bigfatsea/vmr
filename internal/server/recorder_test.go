// Ver 2026-07-20, by Sonnet 5
package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRecorderTTFT: the first non-empty body write stamps the first-token
// latency relative to the start time; later writes don't move it.
func TestRecorderTTFT(t *testing.T) {
	rw := newRecorder(httptest.NewRecorder(), time.Now().Add(-100*time.Millisecond), true)
	if rw.ttftMS != 0 {
		t.Fatalf("ttft before any write = %d", rw.ttftMS)
	}
	rw.Write([]byte("first"))
	got := rw.ttftMS
	if got < 100 || got > 5000 {
		t.Errorf("ttft = %dms, want ~100ms", got)
	}
	time.Sleep(2 * time.Millisecond)
	rw.Write([]byte("second"))
	if rw.ttftMS != got {
		t.Errorf("ttft moved on second write: %d → %d", got, rw.ttftMS)
	}
}

// TestRecorderCapsAuditBodyButNotClientBody locks in recorderBodyCap: the
// client must receive every byte unchanged (a proxy has no business editing
// a response body it didn't originate), while the audit copy stops growing
// past the cap and gets a truncation marker so a human reading the audit log
// knows the body was cut, not that it was really that short.
func TestRecorderCapsAuditBodyButNotClientBody(t *testing.T) {
	under := httptest.NewRecorder()
	rw := newRecorder(under, time.Now(), true)

	const chunk = 1 << 20 // 1MB chunks so the test doesn't allocate 16MB+ in one shot
	p := make([]byte, chunk)
	for i := range p {
		p[i] = 'x'
	}
	total := 0
	for total < recorderBodyCap+5*chunk {
		n, err := rw.Write(p)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		total += n
	}

	if under.Body.Len() != total {
		t.Errorf("client body length = %d, want %d (client must see every byte)", under.Body.Len(), total)
	}

	msg := rw.message()
	body, ok := msg.Body.(string)
	if !ok {
		t.Fatalf("audit body: want string, got %T", msg.Body)
	}
	if len(body) <= recorderBodyCap || len(body) >= total {
		t.Errorf("audit body length = %d, want > %d (cap) and < %d (total written)", len(body), recorderBodyCap, total)
	}
	wantSuffix := "...(recording truncated at 16777216 bytes)"
	if !strings.HasSuffix(body, wantSuffix) {
		t.Errorf("audit body missing truncation marker, tail = %q", body[len(body)-min(len(body), 80):])
	}
}

func TestRecorderUnwrap(t *testing.T) {
	under := httptest.NewRecorder()
	rw := newRecorder(under, time.Now(), true)
	if got := rw.Unwrap(); got != under {
		t.Errorf("Unwrap() = %v, want %v", got, under)
	}
}

// TestRecorderStatusOnlyMode: with captureBody=false (-audit=false + live
// stats) the client still gets every byte and status/ttft are still tracked,
// but nothing accumulates in buf and message() is nil — the completion hook
// only needs status + ttftMS.
func TestRecorderStatusOnlyMode(t *testing.T) {
	under := httptest.NewRecorder()
	rw := newRecorder(under, time.Now().Add(-50*time.Millisecond), false)

	rw.WriteHeader(201)
	big := make([]byte, 4<<20)
	rw.Write(big)
	rw.Write([]byte("more"))

	if under.Body.Len() != len(big)+4 {
		t.Errorf("client body = %d, want %d (client must see every byte)", under.Body.Len(), len(big)+4)
	}
	if rw.buf.Len() != 0 {
		t.Errorf("status-only recorder buffered %d bytes, want 0", rw.buf.Len())
	}
	if rw.status != 201 {
		t.Errorf("status = %d, want 201", rw.status)
	}
	if rw.ttftMS < 50 || rw.ttftMS > 5000 {
		t.Errorf("ttft = %dms, want ~50ms", rw.ttftMS)
	}
	if rw.message() != nil {
		t.Errorf("message() = non-nil in status-only mode, want nil")
	}
}
