package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/metadata"
)

const audimetaID = "audimeta"
const audimetaBaseURL = "https://api.audimeta.de"

// AudiMeta is the Source impl for api.audimeta.de.
type AudiMeta struct {
	http *HTTPClient
}

// NewAudiMeta constructs the source with the production base URL.
func NewAudiMeta(ua string) *AudiMeta {
	return NewAudiMetaAt(audimetaBaseURL, ua)
}

// NewAudiMetaAt constructs the source against a custom base URL (tests).
func NewAudiMetaAt(baseURL, ua string) *AudiMeta {
	return &AudiMeta{http: NewHTTPClient(baseURL, ua)}
}

func (a *AudiMeta) ID() string                       { return audimetaID }
func (a *AudiMeta) Enabled(cfg map[string]bool) bool { return cfg[audimetaID] }

// Search dispatches on the shape of `query`: ASIN regex → book lookup;
// otherwise text → title search endpoint.
func (a *AudiMeta) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	if asinRE.MatchString(query) {
		c, err := a.Get(ctx, query, region)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if c == nil {
			return nil, nil
		}
		return []metadata.Candidate{*c}, nil
	}
	return a.searchByTitle(ctx, query, region)
}

// Get looks up a single book by ASIN.
func (a *AudiMeta) Get(ctx context.Context, asin, region string) (*metadata.Candidate, error) {
	if !asinRE.MatchString(asin) {
		return nil, nil
	}
	bookURL := fmt.Sprintf("%s/books/%s?region=%s",
		a.http.BaseURL, url.PathEscape(asin), url.QueryEscape(region))
	body, err := a.http.GetJSON(ctx, bookURL)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var book audimetaBook
	if err := UnmarshalInto(body, &book); err != nil {
		return nil, err
	}
	c := book.toCandidate(region, body)
	return &c, nil
}

// searchByTitle calls the /search endpoint with a title query.
func (a *AudiMeta) searchByTitle(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	searchURL := fmt.Sprintf("%s/search?q=%s&region=%s",
		a.http.BaseURL, url.QueryEscape(query), url.QueryEscape(region))
	body, err := a.http.GetJSON(ctx, searchURL)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// The response may be a plain array or a wrapped object with results/books/items/data.
	// Try array first.
	var direct []audimetaSearchResult
	if json.Unmarshal(body, &direct) == nil {
		return searchResultsToCandidate(direct, region), nil
	}

	// Try wrapped object.
	var wrapped audimetaSearchResponse
	if err := UnmarshalInto(body, &wrapped); err != nil {
		return nil, err
	}
	results := wrapped.Results
	if results == nil {
		results = wrapped.Books
	}
	if results == nil {
		results = wrapped.Items
	}
	if results == nil {
		results = wrapped.Data
	}
	if results == nil {
		return nil, fmt.Errorf("audimeta: search response contained no recognised key (results/books/items/data) and was not a plain array")
	}
	return searchResultsToCandidate(results, region), nil
}

func searchResultsToCandidate(results []audimetaSearchResult, region string) []metadata.Candidate {
	out := make([]metadata.Candidate, 0, len(results))
	for _, r := range results {
		out = append(out, r.toCandidate(region))
	}
	return out
}

// audimetaBook is the shape of GET /books/<asin>.
type audimetaBook struct {
	ASIN             string          `json:"asin"`
	Title            string          `json:"title"`
	Subtitle         string          `json:"subtitle"`
	Description      string          `json:"description"`
	Summary          string          `json:"summary"`
	Image            string          `json:"image"`
	Language         string          `json:"language"`
	Publisher        string          `json:"publisher"`
	ReleaseDate      string          `json:"releaseDate"`
	RuntimeLengthMin int             `json:"runtimeLengthMin"`
	Authors          []audimetaName  `json:"authors"`
	Narrators        []audimetaName  `json:"narrators"`
	Genres           []audimetaGenre `json:"genres"`
	Series           *audimetaSeries `json:"series,omitempty"`
	SeriesPrimary    *audimetaSeries `json:"seriesPrimary,omitempty"`
	SeriesSecondary  *audimetaSeries `json:"seriesSecondary,omitempty"`
}

// audimetaSearchResult is the shape of items in the /search response.
type audimetaSearchResult struct {
	ASIN             string          `json:"asin"`
	Title            string          `json:"title"`
	Subtitle         string          `json:"subtitle"`
	Image            string          `json:"image"`
	ReleaseDate      string          `json:"releaseDate"`
	RuntimeLengthMin int             `json:"runtimeLengthMin"`
	Authors          []audimetaName  `json:"authors"`
	Narrators        []audimetaName  `json:"narrators"`
	Series           *audimetaSeries `json:"series,omitempty"`
	SeriesPrimary    *audimetaSeries `json:"seriesPrimary,omitempty"`
}

// audimetaSearchResponse handles the wrapped search response envelope.
type audimetaSearchResponse struct {
	Results []audimetaSearchResult `json:"results"`
	Books   []audimetaSearchResult `json:"books"`
	Items   []audimetaSearchResult `json:"items"`
	Data    []audimetaSearchResult `json:"data"`
}

type audimetaName struct {
	Name string `json:"name"`
}
type audimetaGenre struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}
type audimetaSeries struct {
	Name     string `json:"name"`
	Position string `json:"position"`
}

func (b audimetaBook) toCandidate(region string, raw []byte) metadata.Candidate {
	description := b.Description
	if description == "" {
		description = b.Summary
	}
	c := metadata.Candidate{
		Source:      audimetaID,
		ExternalID:  b.ASIN,
		Title:       b.Title,
		Description: description,
		ASIN:        b.ASIN,
		CoverURL:    b.Image,
		PublishedAt: b.ReleaseDate,
		Publisher:   b.Publisher,
		Language:    b.Language,
		RuntimeMin:  b.RuntimeLengthMin,
		Region:      region,
		Raw:         raw,
	}
	for _, a := range b.Authors {
		c.Authors = append(c.Authors, a.Name)
	}
	for _, n := range b.Narrators {
		c.Narrators = append(c.Narrators, n.Name)
	}
	for _, g := range b.Genres {
		c.Genres = append(c.Genres, g.Name)
	}
	// Prefer series > seriesPrimary > seriesSecondary.
	s := b.Series
	if s == nil {
		s = b.SeriesPrimary
	}
	if s == nil {
		s = b.SeriesSecondary
	}
	if s != nil {
		c.Series = s.Name
		c.SeriesPos = s.Position
	}
	return c
}

func (r audimetaSearchResult) toCandidate(region string) metadata.Candidate {
	c := metadata.Candidate{
		Source:      audimetaID,
		ExternalID:  r.ASIN,
		Title:       r.Title,
		ASIN:        r.ASIN,
		CoverURL:    r.Image,
		PublishedAt: r.ReleaseDate,
		RuntimeMin:  r.RuntimeLengthMin,
		Region:      region,
	}
	for _, a := range r.Authors {
		c.Authors = append(c.Authors, a.Name)
	}
	for _, n := range r.Narrators {
		c.Narrators = append(c.Narrators, n.Name)
	}
	s := r.Series
	if s == nil {
		s = r.SeriesPrimary
	}
	if s != nil {
		c.Series = s.Name
		c.SeriesPos = s.Position
	}
	return c
}
