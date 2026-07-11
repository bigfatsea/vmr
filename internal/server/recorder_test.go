// Ver 2026-07-10, by Fable 5
package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestRecorderTTFT: the first non-empty body write stamps the first-token
// latency relative to the start time; later writes don't move it.
func TestRecorderTTFT(t *testing.T) {
	rw := newRecorder(httptest.NewRecorder(), time.Now().Add(-100*time.Millisecond))
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
