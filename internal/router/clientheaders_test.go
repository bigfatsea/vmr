package router

import (
	"net/http"
	"testing"
)

func TestFilterClientHeaders(t *testing.T) {
	in := http.Header{
		"Forwarded":           {"for=192.168.1.10;proto=https"},
		"X-Forwarded-For":     {"192.168.1.10"},
		"X-Real-Ip":           {"192.168.1.10"},
		"User-Agent":          {"test-agent/1.0"},
		"Traceparent":         {"00-abc-def-01"},
		"Content-Type":        {"application/json"},
		"Proxy-Authorization": {"Basic xyz"},
	}
	got := FilterClientHeaders(in)
	for _, k := range []string{"Forwarded", "X-Forwarded-For", "X-Real-Ip", "Proxy-Authorization"} {
		if v := got.Get(k); v != "" {
			t.Errorf("%s = %q, want stripped", k, v)
		}
	}
	for k, want := range map[string]string{
		"User-Agent":   "test-agent/1.0",
		"Traceparent":  "00-abc-def-01",
		"Content-Type": "application/json",
	} {
		if v := got.Get(k); v != want {
			t.Errorf("%s = %q, want %q (ordinary headers must be preserved)", k, v, want)
		}
	}
}
