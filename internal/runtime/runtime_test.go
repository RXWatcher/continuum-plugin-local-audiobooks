package runtime_test

import (
	"context"
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"

	pluginrt "github.com/ContinuumApp/continuum-plugin-local-audiobooks/internal/runtime"
)

func newConfigureRequest(t *testing.T, kv map[string]any) *pluginv1.ConfigureRequest {
	t.Helper()
	entries := make([]*pluginv1.ConfigEntry, 0, len(kv))
	for k, v := range kv {
		s, err := structpb.NewStruct(map[string]any{"value": v})
		if err != nil {
			t.Fatalf("structpb: %v", err)
		}
		entries = append(entries, &pluginv1.ConfigEntry{
			Key:   k,
			Value: s,
		})
	}
	return &pluginv1.ConfigureRequest{Config: entries}
}

func TestConfigure_RequiresDatabaseURL(t *testing.T) {
	var got pluginrt.Config
	s := pluginrt.New(nil, func(c pluginrt.Config) error {
		got = c
		return nil
	})
	_, err := s.Configure(context.Background(), newConfigureRequest(t, map[string]any{
		"library_paths": []any{"/srv/audiobooks"},
	}))
	if err == nil {
		t.Fatalf("expected error for missing database_url, got config %+v", got)
	}
}

func TestConfigure_AllowsEmptyLibraryPaths(t *testing.T) {
	var got pluginrt.Config
	s := pluginrt.New(nil, func(c pluginrt.Config) error {
		got = c
		return nil
	})
	_, err := s.Configure(context.Background(), newConfigureRequest(t, map[string]any{
		"database_url": "postgres://x",
	}))
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got.DatabaseURL != "postgres://x" {
		t.Errorf("DatabaseURL = %q", got.DatabaseURL)
	}
	if len(got.LibraryPaths) != 0 {
		t.Errorf("LibraryPaths = %v", got.LibraryPaths)
	}
}

func TestConfigure_ParsesAllFields(t *testing.T) {
	var got pluginrt.Config
	s := pluginrt.New(nil, func(c pluginrt.Config) error {
		got = c
		return nil
	})
	_, err := s.Configure(context.Background(), newConfigureRequest(t, map[string]any{
		"database_url":           "postgres://x",
		"library_paths":          []any{"/srv/audiobooks", "/mnt/library"},
		"standalone_http_listen": ":7879",
		"stream_signing_secret":  "base64secret",
	}))
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got.DatabaseURL != "postgres://x" {
		t.Errorf("DatabaseURL = %q", got.DatabaseURL)
	}
	if len(got.LibraryPaths) != 2 || got.LibraryPaths[0] != "/srv/audiobooks" {
		t.Errorf("LibraryPaths = %v", got.LibraryPaths)
	}
	if got.StandaloneHTTPListen != ":7879" {
		t.Errorf("StandaloneHTTPListen = %q", got.StandaloneHTTPListen)
	}
	if got.StreamSigningSecret != "base64secret" {
		t.Errorf("StreamSigningSecret = %q", got.StreamSigningSecret)
	}
}

func TestConfigure_RejectsStandaloneWithoutSecret(t *testing.T) {
	s := pluginrt.New(nil, func(c pluginrt.Config) error { return nil })
	_, err := s.Configure(context.Background(), newConfigureRequest(t, map[string]any{
		"database_url":           "postgres://x",
		"library_paths":          []any{"/srv/audiobooks"},
		"standalone_http_listen": ":7879",
	}))
	if err == nil {
		t.Fatal("expected error: standalone_http_listen without stream_signing_secret")
	}
}
