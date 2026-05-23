package scheduler

import (
	"context"
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
)

func TestTaskID(t *testing.T) {
	cases := map[string]string{
		"plugin:42:library_scan":              "library_scan", // real host wire format
		"plugin:7:metadata_enrichment_worker": "metadata_enrichment_worker",
		"library_scan":                        "library_scan", // bare (host integration tests)
	}
	for in, want := range cases {
		if got := taskID(in); got != want {
			t.Errorf("taskID(%q) = %q, want %q", in, got, want)
		}
	}
}

// The host sends TaskKey="plugin:<installationID>:<id>"; dispatch must route
// it. Previously the switch on bare ids hit default -> "unknown task key"
// every tick so library_scan / metadata_enrichment_worker never ran.
func TestRun_RoutesPrefixedKey(t *testing.T) {
	ran := false
	s := New(&Tasks{ScanFn: func(context.Context) (int64, error) { ran = true; return 1, nil }})
	if _, err := s.Run(context.Background(),
		&pluginv1.RunScheduledTaskRequest{TaskKey: "plugin:42:library_scan"}); err != nil {
		t.Fatalf("prefixed key must dispatch; got err=%v", err)
	}
	if !ran {
		t.Fatal("library_scan was not invoked for the prefixed key")
	}
}

func TestRun_UnknownKeyStillErrors(t *testing.T) {
	s := New(&Tasks{})
	if _, err := s.Run(context.Background(),
		&pluginv1.RunScheduledTaskRequest{TaskKey: "plugin:42:bogus"}); err == nil {
		t.Fatal("unknown key must still error")
	}
}
