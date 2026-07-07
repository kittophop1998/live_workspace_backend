// Package httpexec performs a single outbound HTTP request on behalf of the
// workspace (a server-side proxy). It backs both the single-endpoint "Try it"
// tester and every step of an E2E workflow run: browsers can't call arbitrary
// hosts (CORS) and can't reliably measure server-observed latency, so the round
// trip happens here. It is deliberately dependency-free (net/http only).
package httpexec

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"kingdom_manager/backend/internal/domain/port"
)

// DefaultTimeout bounds a single proxied request.
const DefaultTimeout = 20 * time.Second

// maxBodyBytes caps how much of a response body we read back to the client so a
// huge download can't exhaust memory.
const maxBodyBytes = 2 << 20 // 2 MiB

// Request is a normalized outbound call.
type Request = port.HTTPRequest

// Response is the observed result of a proxied call.
type Response = port.HTTPResponse

// Executor runs proxied requests. AllowPrivate controls the SSRF guard: when
// false (production-ish), requests to loopback/link-local/private hosts are
// rejected; dev defaults to true so it can hit localhost APIs.
type Executor struct {
	client       *http.Client
	AllowPrivate bool
}

// New builds an Executor. allowPrivate=true is the sensible dev default.
func New(allowPrivate bool) *Executor {
	return &Executor{
		client: &http.Client{
			Timeout: DefaultTimeout,
			// Don't auto-follow redirects — surfacing the 3xx is more useful when
			// validating API behavior.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		AllowPrivate: allowPrivate,
	}
}

// Exec performs the request and returns what the target answered. A transport
// error (DNS, refused, timeout) is returned as a non-nil error; an HTTP error
// status (4xx/5xx) is a normal Response.
func (e *Executor) Exec(ctx context.Context, in Request) (Response, error) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	parsed, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Response{}, fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Response{}, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if !e.AllowPrivate && isPrivateHost(parsed.Hostname()) {
		return Response{}, fmt.Errorf("requests to private/loopback hosts are blocked")
	}

	var bodyReader io.Reader
	if len(in.Body) > 0 {
		bodyReader = bytes.NewReader(in.Body)
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), bodyReader)
	if err != nil {
		return Response{}, err
	}
	for key, value := range in.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		request.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := e.client.Do(request)
	elapsed := time.Since(start)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	// The target may compress the body. Because the tester sets Accept-Encoding
	// explicitly, Go's transport does NOT transparently decode the response, so
	// do it here — otherwise the client sees raw gzip/deflate bytes (which the
	// JSON envelope then mangles into U+FFFD). The original Content-Encoding /
	// Content-Length / Vary headers stay in resp.Header for the response viewer.
	body, err := decodedBody(resp)
	if err != nil {
		return Response{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return Response{}, err
	}
	truncated := len(raw) > maxBodyBytes
	if truncated {
		raw = raw[:maxBodyBytes]
	}

	return Response{
		Status:     resp.StatusCode,
		DurationMs: elapsed.Milliseconds(),
		Headers:    resp.Header,
		Body:       string(raw),
		BodySize:   len(raw),
		Truncated:  truncated,
	}, nil
}

// decodedBody returns a reader over the response body, transparently
// decompressing the encodings the tester advertises (gzip, deflate) using the
// standard library only. Unrecognized encodings (e.g. br) pass through as-is.
func decodedBody(resp *http.Response) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "gzip":
		return gzip.NewReader(resp.Body)
	case "deflate":
		return flate.NewReader(resp.Body), nil
	default:
		return resp.Body, nil
	}
}

// isPrivateHost reports whether a host resolves to a loopback/link-local/private
// address (best-effort SSRF guard for non-dev deployments).
func isPrivateHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ips := []net.IP{}
	if ip := net.ParseIP(host); ip != nil {
		ips = append(ips, ip)
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil {
			return true // can't resolve → treat as unsafe
		}
		ips = resolved
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}
