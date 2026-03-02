package middleware

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ip := clientIP(r)
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path = path + "?" + r.URL.RawQuery
		}

		log.Printf("REQ start method=%s path=%s ip=%s ua=%q", r.Method, path, ip, r.UserAgent())

		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf(
			"REQ done method=%s path=%s status=%d bytes=%d duration=%s",
			r.Method,
			path,
			status,
			recorder.bytes,
			time.Since(start),
		)
	})
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
