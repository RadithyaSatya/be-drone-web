package ws

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type countingResponseWriter struct {
	header          http.Header
	status          int
	writeHeaderCall int
	body            bytes.Buffer
}

func (w *countingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *countingResponseWriter) WriteHeader(status int) {
	w.writeHeaderCall++
	if w.status == 0 {
		w.status = status
	}
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(p)
}

func TestServeHTTPFailedUpgradeWritesHeaderOnce(t *testing.T) {
	handler := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/ws/telemetry?token=test", nil)
	recorder := &countingResponseWriter{}

	handler.ServeHTTP(recorder, req)

	if recorder.status != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", recorder.status, http.StatusBadRequest)
	}
	if recorder.writeHeaderCall != 1 {
		t.Fatalf("WriteHeader called %d times, want 1", recorder.writeHeaderCall)
	}
	if strings.Contains(recorder.body.String(), "failed to upgrade websocket") {
		t.Fatalf("unexpected fallback error body: %q", recorder.body.String())
	}
}
