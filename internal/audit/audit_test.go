// Ver 2026-07-07, by Fable 5
package audit

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactMasksCredentials(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-secret-key-abcd")
	h.Set("x-api-key", "sk-another-secret-wxyz")
	h.Set("Content-Type", "application/json")
	out := Redact(h)
	if got := out.Get("Authorization"); got != "Bearer ***abcd" {
		t.Errorf("authorization: %q", got)
	}
	if got := out.Get("X-Api-Key"); got != "***wxyz" {
		t.Errorf("x-api-key: %q", got)
	}
	if got := out.Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type must not be masked: %q", got)
	}
	// Original untouched.
	if h.Get("Authorization") != "Bearer sk-secret-key-abcd" {
		t.Error("Redact must not mutate the input")
	}
}

func TestEncodeBody(t *testing.T) {
	if b, tr := EncodeBody([]byte(`{"a":1}`)); tr || string(b.(json.RawMessage)) != `{"a":1}` {
		t.Errorf("json body: %v %v", b, tr)
	}
	if b, tr := EncodeBody([]byte("data: hello\n\n")); tr || b.(string) != "data: hello\n\n" {
		t.Errorf("sse body: %v %v", b, tr)
	}
	if b, tr := EncodeBody(nil); b != nil || tr {
		t.Errorf("empty body: %v %v", b, tr)
	}
	big := strings.Repeat("x", int(MaxBodyBytes())+100)
	b, tr := EncodeBody([]byte(big))
	if !tr || len(b.(string)) != int(MaxBodyBytes()) {
		t.Errorf("truncation: tr=%v len=%d", tr, len(b.(string)))
	}
}

func TestDailyRotation(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	day1 := time.Date(2026, 7, 7, 23, 59, 0, 0, time.Local)
	l.now = func() time.Time { return day1 }
	if err := l.Write(&Record{TS: day1, Model: "m1"}); err != nil {
		t.Fatal(err)
	}
	day2 := day1.Add(2 * time.Minute)
	l.now = func() time.Time { return day2 }
	if err := l.Write(&Record{TS: day2, Model: "m2"}); err != nil {
		t.Fatal(err)
	}

	for date, model := range map[string]string{"2026-07-07": "m1", "2026-07-08": "m2"} {
		data, err := os.ReadFile(filepath.Join(dir, "vmr-audit-"+date+".jsonl"))
		if err != nil {
			t.Fatalf("%s: %v", date, err)
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil || rec.Model != model {
			t.Errorf("%s: %v model=%s", date, err, rec.Model)
		}
	}
}

func TestNilLoggerNoop(t *testing.T) {
	var l *Logger
	if err := l.Write(&Record{}); err != nil {
		t.Error(err)
	}
	if err := l.Close(); err != nil {
		t.Error(err)
	}
}

func TestDirEnv(t *testing.T) {
	t.Setenv("VMR_LOG_DIR", "/some/dir")
	if Dir() != "/some/dir" {
		t.Errorf("env dir: %s", Dir())
	}
	t.Setenv("VMR_LOG_DIR", "")
	if Dir() != os.TempDir() {
		t.Errorf("default dir: %s", Dir())
	}
}
