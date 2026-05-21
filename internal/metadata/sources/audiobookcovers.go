package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/metadata"
)

const audiobookcoversID = "audiobookcovers"
const audiobookcoversBaseURL = "https://api.audiobookcovers.com"

// AudiobookCovers is the Source impl for api.audiobookcovers.com.
// It is cover-only: returned Candidates have no Title, Authors, Narrators, etc.
// The aggregator's confidence formula will naturally rank them low, surfacing
// them as "improve cover" candidates rather than primary matches.
type AudiobookCovers struct {
	http *HTTPClient
}

// NewAudiobookCovers constructs the source with the production base URL.
func NewAudiobookCovers(ua string) *AudiobookCovers {
	return NewAudiobookCoversAt(audiobookcoversBaseURL, ua)
}

// NewAudiobookCoversAt constructs the source against a custom base URL (tests).
func NewAudiobookCoversAt(baseURL, ua string) *AudiobookCovers {
	return &AudiobookCovers{http: NewHTTPClient(baseURL, ua)}
}

func (a *AudiobookCovers) ID() string                       { return audiobookcoversID }
func (a *AudiobookCovers) Enabled(cfg map[string]bool) bool { return cfg[audiobookcoversID] }

// Get fetches a cover record for the given ASIN.
// Returns (nil, nil) for any input that is not an ASIN — Audiobookcovers
// only supports ASIN-based lookup.
func (a *AudiobookCovers) Get(ctx context.Context, asin, region string) (*metadata.Candidate, error) {
	if !asinRE.MatchString(asin) {
		return nil, nil
	}
	coverURL := fmt.Sprintf("%s/cover/by_book/%s", a.http.BaseURL, asin)
	body, err := a.http.GetJSON(ctx, coverURL)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var resp audiobookcoversResponse
	if err := UnmarshalInto(body, &resp); err != nil {
		return nil, err
	}
	if resp.CoverURL == "" {
		return nil, ErrNotFound
	}
	c := metadata.Candidate{
		Source:     audiobookcoversID,
		ExternalID: asin,
		ASIN:       asin,
		CoverURL:   resp.CoverURL,
		Raw:        json.RawMessage(body),
	}
	return &c, nil
}

// Search dispatches on the shape of query: ASIN → delegates to Get;
// text query → returns (nil, nil) because Audiobookcovers has no text search.
func (a *AudiobookCovers) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	if !asinRE.MatchString(query) {
		return nil, nil
	}
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

// audiobookcoversResponse is the shape of GET /cover/by_book/<asin>.
type audiobookcoversResponse struct {
	ASIN     string `json:"asin"`
	CoverURL string `json:"cover_url"`
}
