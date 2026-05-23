package metadataprovider

import (
	"context"
	"encoding/json"
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/metadata"
	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/metadata/sources"
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

// capturingAggregator captures the original argument passed to Search.
type capturingAggregator struct {
	capturedOriginal *metadata.Candidate
}

func (a *capturingAggregator) Search(_ context.Context, _, _ string, _ map[string]bool, original *metadata.Candidate) ([]metadata.Match, error) {
	a.capturedOriginal = original
	return nil, nil
}

func newServerLite() *Server {
	src := &fakeSrc{id: "audnexus", cand: &metadata.Candidate{
		Source:     "audnexus",
		ExternalID: "B0EXAMPLE",
		Title:      "X",
		CoverURL:   "https://example/c.jpg",
	}}
	s := &Server{}
	s.SetEnabled(func() map[string]bool { return map[string]bool{"audnexus": true} })
	s.SetRegion(func() string { return "us" })
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

// TestServer_Search_ProviderIDs_OriginalCandidate verifies that when
// req.ProviderIds contains an ASIN, Search is called with a non-nil original
// so the confidence scorer can award ASIN-match bonus points.
func TestServer_Search_ProviderIDs_OriginalCandidate(t *testing.T) {
	cap := &capturingAggregator{}
	s := &Server{}
	s.SetEnabled(func() map[string]bool { return nil })
	s.SetRegion(func() string { return "us" })
	s.SetAggregator(cap)

	pids, err := structpb.NewStruct(map[string]any{"asin": "B0TESTVALUE"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:       "some audiobook",
		ItemType:    "audiobook",
		ProviderIds: pids,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.capturedOriginal == nil {
		t.Fatal("expected non-nil original to be passed to agg.Search")
	}
	if cap.capturedOriginal.ASIN != "B0TESTVALUE" {
		t.Errorf("expected ASIN %q, got %q", "B0TESTVALUE", cap.capturedOriginal.ASIN)
	}
}

// TestCandidateToMetadataItem_ChaptersAndExtras verifies that a Candidate with
// chapters, ASIN, and source is mapped to a MetadataItem with extras.chapters_json,
// extras.source, and extras.asin populated correctly.
func TestCandidateToMetadataItem_ChaptersAndExtras(t *testing.T) {
	cand := metadata.Candidate{
		Source:     "audnexus",
		ExternalID: "B0TEST",
		Title:      "Test Book",
		ASIN:       "B0TEST",
		Chapters: []metadata.Chapter{
			{Title: "Intro", StartMS: 0, EndMS: 5000},
			{Title: "Chapter 1", StartMS: 5000, EndMS: 60000},
		},
	}
	item, err := candidateToMetadataItem(cand, "audnexus:B0TEST")
	if err != nil {
		t.Fatal(err)
	}
	meta := item.GetMetadata().AsMap()

	// extras.chapters_json must be present and valid JSON matching the chapters.
	chapRaw, ok := meta["chapters_json"].(string)
	if !ok || chapRaw == "" {
		t.Fatalf("expected chapters_json string in extras, got %v", meta["chapters_json"])
	}
	var chapters []metadata.Chapter
	if err := json.Unmarshal([]byte(chapRaw), &chapters); err != nil {
		t.Fatalf("chapters_json not valid JSON: %v", err)
	}
	if len(chapters) != 2 || chapters[0].Title != "Intro" || chapters[1].Title != "Chapter 1" {
		t.Errorf("unexpected chapters: %+v", chapters)
	}

	// extras.source must equal "audnexus".
	if meta["source"] != "audnexus" {
		t.Errorf("extras.source = %v, want audnexus", meta["source"])
	}

	// extras.asin must equal "B0TEST".
	if meta["asin"] != "B0TEST" {
		t.Errorf("extras.asin = %v, want B0TEST", meta["asin"])
	}
}

// TestStripHTML verifies that HTML tags are removed and entities are decoded.
func TestStripHTML(t *testing.T) {
	input := "<p>Hello <b>world</b>&amp;more</p>"
	want := "Hello world&more"
	got := stripHTML(input)
	if got != want {
		t.Errorf("stripHTML(%q) = %q, want %q", input, got, want)
	}
}

// TestServer_Search_ProviderIDs_NoSignals verifies that when provider_ids
// contains no recognized fields, original remains nil (no spurious candidate).
func TestServer_Search_ProviderIDs_NoSignals(t *testing.T) {
	cap := &capturingAggregator{}
	s := &Server{}
	s.SetEnabled(func() map[string]bool { return nil })
	s.SetRegion(func() string { return "us" })
	s.SetAggregator(cap)

	pids, err := structpb.NewStruct(map[string]any{"unknown_key": "irrelevant"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:       "some audiobook",
		ProviderIds: pids,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.capturedOriginal != nil {
		t.Errorf("expected nil original when no recognized fields present, got %+v", cap.capturedOriginal)
	}
}
