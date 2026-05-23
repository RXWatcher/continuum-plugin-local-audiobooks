package httproutes

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeHTTPBeforeSetHandlerReturns503(t *testing.T) {
	s := NewServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"not_ready"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestServeHTTPStripsSiloHeaders(t *testing.T) {
	var seen http.Header
	srv := NewServer()
	srv.SetHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Silo-User-Id", "forged")
	req.Header.Set("X-Silo-User-Role", "admin")
	req.Header.Set("x-silo-theme", "dark") // lowercase variant
	req.Header.Set("Authorization", "Bearer keep-me")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	srv.ServeHTTP(httptest.NewRecorder(), req)
	for k := range seen {
		if strings.HasPrefix(strings.ToLower(k), "x-silo-") {
			t.Errorf("header %q leaked through to handler", k)
		}
	}
	if got := seen.Get("Authorization"); got != "Bearer keep-me" {
		t.Errorf("Authorization = %q, want it preserved", got)
	}
	if got := seen.Get("X-Forwarded-For"); got != "1.2.3.4" {
		t.Errorf("X-Forwarded-For = %q, want it preserved", got)
	}
}

func TestStandaloneListenerRoutesThrough(t *testing.T) {
	srv := NewServer()
	srv.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	hs := &http.Server{Handler: srv, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_ = hs.Shutdown(ctx)
	})
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + ln.Addr().String() + "/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
