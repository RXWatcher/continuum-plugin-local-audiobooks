// Adapters bridge *sources.Registry to the metadata package's interface types.
//
// The sources package imports metadata (for Candidate), so metadata cannot
// import sources without creating a cycle. The metadata package therefore
// declares its own Source / SourceRegistry / EnrichmentRegistry interfaces.
// These thin adapter types convert between the two worlds.
package main

import (
	"context"
	"errors"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/metadata"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/metadata/sources"
)

// -- aggregatorRegistryAdapter --

// aggregatorRegistryAdapter adapts *sources.Registry to metadata.SourceRegistry.
type aggregatorRegistryAdapter struct{ reg *sources.Registry }

func newAggregatorRegistryAdapter(reg *sources.Registry) *aggregatorRegistryAdapter {
	return &aggregatorRegistryAdapter{reg: reg}
}

func (a *aggregatorRegistryAdapter) All() []metadata.Source {
	src := a.reg.All() // []sources.Source
	out := make([]metadata.Source, len(src))
	for i, s := range src {
		out[i] = sourceAdapter{s: s}
	}
	return out
}

// -- workerRegistryAdapter --

// workerRegistryAdapter adapts *sources.Registry to metadata.EnrichmentRegistry.
type workerRegistryAdapter struct{ reg *sources.Registry }

func newWorkerRegistryAdapter(reg *sources.Registry) *workerRegistryAdapter {
	return &workerRegistryAdapter{reg: reg}
}

func (w *workerRegistryAdapter) ForID(id string) metadata.Source {
	s := w.reg.ForID(id)
	if s == nil {
		return nil
	}
	return sourceAdapter{s: s}
}

// -- sourceAdapter --

// sourceAdapter wraps a sources.Source as a metadata.Source. Its Get method
// translates sources.ErrNotFound to metadata.ErrNotFound so the enrichment
// worker's errors.Is(err, metadata.ErrNotFound) check works correctly.
type sourceAdapter struct{ s sources.Source }

func (a sourceAdapter) ID() string                       { return a.s.ID() }
func (a sourceAdapter) Enabled(cfg map[string]bool) bool { return a.s.Enabled(cfg) }

func (a sourceAdapter) Get(ctx context.Context, id, region string) (*metadata.Candidate, error) {
	c, err := a.s.Get(ctx, id, region)
	if errors.Is(err, sources.ErrNotFound) {
		return nil, metadata.ErrNotFound
	}
	return c, err
}

func (a sourceAdapter) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	// Search returns empty slice on not-found; no error mapping needed.
	return a.s.Search(ctx, query, region)
}
