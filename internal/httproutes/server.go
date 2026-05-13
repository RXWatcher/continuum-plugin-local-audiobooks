// Package httproutes adapts a stdlib http.Handler to the SDK's HttpRoutes.v1
// gRPC service. The plugin host invokes our gRPC service for each inbound
// HTTP request; we replay against the wrapped handler and return its
// response. ServeHTTP exposes the same handler to a standalone HTTP
// listener with X-Continuum-* header stripping (security: host-trust
// headers cannot reach handlers from the public listener).
package httproutes

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
)

type Server struct {
	pluginv1.UnimplementedHttpRoutesServer
	handler atomic.Pointer[http.Handler]
}

func NewServer() *Server { return &Server{} }

func (s *Server) SetHandler(h http.Handler) {
	if h == nil {
		s.handler.Store(nil)
		return
	}
	s.handler.Store(&h)
}

// ServeHTTP is used by the standalone listener. Strips X-Continuum-*
// headers before invoking the wrapped handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hPtr := s.handler.Load()
	if hPtr == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"not_ready","message":"plugin not configured"}}`))
		return
	}
	for k := range r.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-continuum-") {
			r.Header.Del(k)
		}
	}
	(*hPtr).ServeHTTP(w, r)
}

// Handle is the gRPC HttpRoutes.v1 RPC — used when the host plugin proxy
// forwards a request via gRPC instead of direct HTTP.
func (s *Server) Handle(_ context.Context, req *pluginv1.HandleHTTPRequest) (*pluginv1.HandleHTTPResponse, error) {
	hPtr := s.handler.Load()
	if hPtr == nil {
		return &pluginv1.HandleHTTPResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       []byte(`{"error":{"code":"not_ready","message":"plugin not configured"}}`),
			Headers:    map[string]string{"Content-Type": "application/json"},
		}, nil
	}
	h := *hPtr

	rawQuery := ""
	if req.GetQuery() != nil {
		vals := url.Values{}
		for k, v := range req.GetQuery().GetFields() {
			if sv := v.GetStringValue(); sv != "" {
				vals.Set(k, sv)
				continue
			}
			vals.Set(k, v.String())
		}
		rawQuery = vals.Encode()
	}
	u := &url.URL{Path: req.GetPath(), RawQuery: rawQuery}
	method := req.GetMethod()
	if method == "" {
		method = http.MethodGet
	}
	httpReq := httptest.NewRequest(method, u.String(), bytes.NewReader(req.GetBody()))
	for k, v := range req.GetHeaders() {
		httpReq.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httpReq)

	body, _ := io.ReadAll(io.LimitReader(rec.Result().Body, 64<<20))
	headers := map[string]string{}
	for k, vs := range rec.Header() {
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}
	return &pluginv1.HandleHTTPResponse{
		StatusCode: int32(rec.Code),
		Headers:    headers,
		Body:       body,
	}, nil
}
