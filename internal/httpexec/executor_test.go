package httpexec

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
