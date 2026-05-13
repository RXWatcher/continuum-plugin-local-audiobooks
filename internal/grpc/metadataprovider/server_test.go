package metadataprovider

import (
	"context"
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/metadata"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/metadata/sources"
)

// fakeSrc satisfies sources.Source for tests.
type fakeSrc struct {
	id   string
	cand *metadata.Candidate
}

func (f *fakeSrc) ID() string                     { return f.id }
func (f *fakeSrc) Enabled(_ map[string]bool) bool { return true }
func (f *fakeSrc) Get(_ context.Context, _, _ string) (*metadata.Candidate, error) {
	return f.cand, nil
}
func (f *fakeSrc) Search(_ context.Context, _, _ string) ([]metadata.Candidate, error) {
	if f.cand == nil {
		return nil, nil
	}
	return []metadata.Candidate{*f.cand}, nil
}

// fakeRegistry satisfies SourceLookup.
type fakeRegistry struct{ s sources.Source }

func (r *fakeRegistry) ForID(id string) sources.Source {
	if r.s != nil && r.s.ID() == id {
		return r.s
	}
	return nil
}

// fakeAggregator satisfies MetadataAggregator.
type fakeAggregator struct{ matches []metadata.Match }

func (a *fakeAggregator) Search(_ context.Context, _, _ string, _ map[string]bool, _ *metadata.Candidate) ([]metadata.Match, error) {
	return a.matches, nil
}

func newServerLite() *Server {
	src := &fakeSrc{id: "audnexus", cand: &metadata.Candidate{
		Source:     "audnexus",
		ExternalID: "B0EXAMPLE",
		Title:      "X",
		CoverURL:   "https://example/c.jpg",
	}}
	s := &Server{
		Enabled: func() map[string]bool { return map[string]bool{"audnexus": true} },
		Region:  func() string { return "us" },
	}
	s.SetAggregator(&fakeAggregator{matches: []metadata.Match{{
		Source:     "audnexus",
		Confidence: 50,
		Candidate:  metadata.Candidate{Source: "audnexus", ExternalID: "B0EXAMPLE", Title: "X"},
	}}})
	s.SetRegistry(&fakeRegistry{s: src})
	return s
}

func TestServer_GetMetadata_HappyPath(t *testing.T) {
	s := newServerLite()
	resp, err := s.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ProviderId: "audnexus:B0EXAMPLE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetItem().GetTitle() != "X" {
		t.Errorf("title %q", resp.GetItem().GetTitle())
	}
}

func TestServer_GetMetadata_BadExternalID(t *testing.T) {
	s := newServerLite()
	_, err := s.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ProviderId: "noprefix",
	})
	if err == nil {
		t.Errorf("expected error")
	}
}

func TestServer_GetMetadata_SourceNotFound(t *testing.T) {
	s := newServerLite()
	_, err := s.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ProviderId: "unknownsource:abc",
	})
	if err == nil {
		t.Errorf("expected NotFound error")
	}
}

func TestServer_GetImages(t *testing.T) {
	s := newServerLite()
	resp, err := s.GetImages(context.Background(), &pluginv1.GetImagesRequest{
		ProviderId: "audnexus:B0EXAMPLE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetImages()) == 0 {
		t.Errorf("expected at least one image")
	}
	if resp.GetImages()[0].GetUrl() != "https://example/c.jpg" {
		t.Errorf("unexpected image url %q", resp.GetImages()[0].GetUrl())
	}
}

func TestServer_ResolveImageURL_Passthrough(t *testing.T) {
	s := newServerLite()
	resp, err := s.ResolveImageURL(context.Background(), &pluginv1.ResolveImageURLRequest{
		Path: "https://example/x.jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetUrl() != "https://example/x.jpg" {
		t.Errorf("got %q", resp.GetUrl())
	}
}

func TestServer_Search_NonAudiobookEmpty(t *testing.T) {
	s := newServerLite()
	resp, err := s.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:    "anything",
		ItemType: "movie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetResults()) != 0 {
		t.Errorf("expected 0 results for movie itemType")
	}
}

func TestServer_Search_HappyPath(t *testing.T) {
	s := newServerLite()
	resp, err := s.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:    "X",
		ItemType: "audiobook",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetResults()) != 1 {
		t.Errorf("expected 1 result, got %d", len(resp.GetResults()))
	}
}
