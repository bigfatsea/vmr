// Regression and security tests: Slowloris slow body reading mitigation.
package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSlowloris_BodyReadTimeout(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, ""))

	// Temporarily tighten read timeout for fast test execution.
	setBodyReadTimeout(150 * time.Millisecond)
	defer setBodyReadTimeout(60 * time.Second)

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send HTTP headers declaring 1000 bytes body, but send only an incomplete chunk and stall.
	reqHeaders := fmt.Sprintf("POST /v1/chat/completions HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 1000\r\nConnection: close\r\n\r\n{\"model\":\"vm\",", ts.Listener.Addr().String())
	if _, err := conn.Write([]byte(reqHeaders)); err != nil {
		t.Fatalf("write headers: %v", err)
	}

	// Wait for deadline on the server side and read response.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBytes, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	respStr := string(respBytes)
	if !strings.Contains(respStr, "400 Bad Request") {
		t.Errorf("expected 400 Bad Request, got response: %s", respStr)
	}
	if !strings.Contains(respStr, "failed to read request body") {
		t.Errorf("expected 'failed to read request body' error message, got: %s", respStr)
	}
}

func TestSlowloris_NormalBodyReadsSuccessfully(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestSlowloris_SlowReaderWithinTimeout(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, ""))

	setBodyReadTimeout(500 * time.Millisecond)
	defer setBodyReadTimeout(60 * time.Second)

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	payload := `{"model":"vm","messages":[{"role":"user","content":"hi"}]}`
	reqHeaders := fmt.Sprintf("POST /v1/chat/completions HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", ts.Listener.Addr().String(), len(payload))
	if _, err := conn.Write([]byte(reqHeaders)); err != nil {
		t.Fatalf("write headers: %v", err)
	}

	// Send body in two chunks with a short delay well below bodyReadTimeout.
	half := len(payload) / 2
	if _, err := conn.Write([]byte(payload[:half])); err != nil {
		t.Fatalf("write chunk 1: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := conn.Write([]byte(payload[half:])); err != nil {
		t.Fatalf("write chunk 2: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBytes, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	respStr := string(respBytes)
	if !strings.Contains(respStr, "200 OK") {
		t.Fatalf("expected 200 OK, got: %s", respStr)
	}
}
