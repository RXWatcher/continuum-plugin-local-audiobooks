// Package scheduler implements the scheduled_task.v1 RPC. Currently a
// single task: library_scan.
package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
)

// taskID extracts the capability id from a scheduled-task key. The Silo
// host sends "plugin:<installationID>:<capabilityID>" (task_registry
// pluginTaskKey); bare ids may arrive from host integration tests. This
// plugin's task ids ("library_scan", "metadata_enrichment_worker") contain
// no ':'.
func taskID(key string) string {
	if i := strings.LastIndexByte(key, ':'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// Tasks holds the registered task functions so both the admin trigger and the
// scheduled-task RPC run the same code path. ScanFn returns the
// scan_event id; concurrent triggers de-duplicate (the in-flight call's
// id is returned to subsequent callers). DrainFn drains the enrichment queue.
type Tasks struct {
	ScanFn  func(context.Context) (int64, error)
	DrainFn func(context.Context) error

	mu       sync.Mutex
	running  atomic.Bool // guards library_scan
	draining atomic.Bool // guards metadata_enrichment_worker
}

// Server implements ScheduledTaskServer.
type Server struct {
	pluginv1.UnimplementedScheduledTaskServer
	t *Tasks
}

func New(t *Tasks) *Server { return &Server{t: t} }

func (s *Server) Run(ctx context.Context, req *pluginv1.RunScheduledTaskRequest) (*pluginv1.RunScheduledTaskResponse, error) {
	switch taskID(req.GetTaskKey()) {
	case "library_scan":
		if s.t == nil || s.t.ScanFn == nil {
			return &pluginv1.RunScheduledTaskResponse{}, nil
		}
		if !s.t.running.CompareAndSwap(false, true) {
			// Previous scan still running; drop this trigger.
			return &pluginv1.RunScheduledTaskResponse{}, nil
		}
		defer s.t.running.Store(false)
		_, err := s.t.ScanFn(ctx)
		return &pluginv1.RunScheduledTaskResponse{}, err

	case "metadata_enrichment_worker":
		if s.t == nil || s.t.DrainFn == nil {
			return &pluginv1.RunScheduledTaskResponse{}, nil
		}
		if !s.t.draining.CompareAndSwap(false, true) {
			// Previous drain still running (cron is every minute, a drain
			// can take longer). Drop this trigger; the claim lease is the
			// real correctness guard, this just avoids pile-up.
			return &pluginv1.RunScheduledTaskResponse{}, nil
		}
		defer s.t.draining.Store(false)
		err := s.t.DrainFn(ctx)
		return &pluginv1.RunScheduledTaskResponse{}, err

	default:
		return nil, errors.New("unknown task key")
	}
}
