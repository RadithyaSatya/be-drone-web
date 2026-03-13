package middleware

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type hijackableResponseWriter struct {
	header   http.Header
	hijacked bool
	server   net.Conn
	client   net.Conn
}

func (w *hijackableResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *hijackableResponseWriter) WriteHeader(int) {}

func (w *hijackableResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	w.server, w.client = net.Pipe()
	brw := bufio.NewReadWriter(bufio.NewReader(w.server), bufio.NewWriter(w.server))
	return w.server, brw, nil
}

func TestLoggingMiddlewarePreservesHijacker(t *testing.T) {
	base := &hijackableResponseWriter{}
	req := httptest.NewRequest(http.MethodGet, "/ws/telemetry", nil)

	handler := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("wrapped response writer does not implement http.Hijacker")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		_ = conn.Close()
	}))

	handler.ServeHTTP(base, req)

	if !base.hijacked {
		t.Fatal("expected underlying response writer to be hijacked")
	}
	if base.client != nil {
		_ = base.client.Close()
	}
}
