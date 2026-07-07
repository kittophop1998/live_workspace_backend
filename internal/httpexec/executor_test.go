package httpexec

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestExecSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"method":"` + r.Method + `","received":"` + string(body) + `"}`))
	}))
	defer server.Close()

	exec := New(true) // allow loopback for the test server
	resp, err := exec.Exec(context.Background(), Request{
		Method: "POST", URL: server.URL, Headers: map[string]string{"Content-Type": "application/json"}, Body: []byte("hello"),
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if resp.Status != http.StatusCreated {
		t.Errorf("status = %d", resp.Status)
	}
	if !strings.Contains(resp.Body, `"received":"hello"`) {
		t.Errorf("body = %q", resp.Body)
	}
	if values := resp.Headers["X-Test"]; len(values) != 1 || values[0] != "yes" {
		t.Errorf("headers = %+v", resp.Headers)
	}
	if resp.DurationMs < 0 {
		t.Errorf("duration = %d", resp.DurationMs)
	}
}

func TestExecDecodesGzip(t *testing.T) {
	const payload = `{"message":"gzip decoded ok"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte(payload))
		_ = gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	// Set Accept-Encoding explicitly (like the tester) so Go's transport does not
	// transparently decode — the executor must decode it itself.
	resp, err := New(true).Exec(context.Background(), Request{
		Method: "GET", URL: server.URL, Headers: map[string]string{"Accept-Encoding": "gzip, deflate"},
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if resp.Body != payload {
		t.Errorf("body = %q, want decoded %q", resp.Body, payload)
	}
	// Original encoding headers stay visible for the response viewer.
	if v := resp.Headers["Content-Encoding"]; len(v) != 1 || v[0] != "gzip" {
		t.Errorf("Content-Encoding header lost: %+v", resp.Headers)
	}
}

func TestExecInvalidURL(t *testing.T) {
	if _, err := New(true).Exec(context.Background(), Request{URL: "notaurl"}); err == nil {
		t.Error("want error for invalid URL")
	}
}

func TestExecBlocksPrivateWhenDisallowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	// httptest binds to 127.0.0.1 — a loopback address the guard must reject.
	if _, err := New(false).Exec(context.Background(), Request{URL: server.URL}); err == nil {
		t.Error("want error when private hosts are blocked")
	}
}
