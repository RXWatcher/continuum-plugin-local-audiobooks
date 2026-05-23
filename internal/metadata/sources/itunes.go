package sources

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/metadata"
)

const itunesID = "itunes"
const itunesBaseURL = "https://itunes.apple.com"

// ITunes is the Source impl for itunes.apple.com.
// iTunes uses a numeric trackId/collectionId rather than Amazon ASINs.
// The lookup endpoint supports trackId; collectionId is handled as a fallback
// in the JSON parsing (prefer trackId; fall back to collectionId when absent).
type ITunes struct {
	http *HTTPClient
}

// NewITunes constructs the source with the production base URL.
func NewITunes(ua string) *ITunes {
	return NewITunesAt(itunesBaseURL, ua)
}

// NewITunesAt constructs the source against a custom base URL (tests).
func NewITunesAt(baseURL, ua string) *ITunes {
	return &ITunes{http: NewHTTPClient(baseURL, ua)}
}

func (it *ITunes) ID() string                       { return itunesID }
func (it *ITunes) Enabled(cfg map[string]bool) bool { return cfg[itunesID] }

// Get looks up a single audiobook by its iTunes numeric ID.
// Returns (nil, nil) when query is an Amazon ASIN — iTunes cannot resolve those.
func (it *ITunes) Get(ctx context.Context, id, region string) (*metadata.Candidate, error) {
	if asinRE.MatchString(id) {
		return nil, nil
	}
	lookupURL := fmt.Sprintf("%s/lookup?id=%s&country=%s",
		it.http.BaseURL, url.QueryEscape(id), url.QueryEscape(region))
	body, err := it.http.GetJSON(ctx, lookupURL)
	if err != nil {
		return nil, err
	}
	var envelope itunesEnvelope
	if err := UnmarshalInto(body, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Results) == 0 {
		return nil, ErrNotFound
	}
	c := envelope.Results[0].toCandidate(region, body)
	return &c, nil
}

// Search queries iTunes for audiobooks matching the given text.
// Returns (nil, nil) when query is an Amazon ASIN — iTunes cannot resolve those.
func (it *ITunes) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	if asinRE.MatchString(query) {
		return nil, nil
	}
	searchURL := fmt.Sprintf("%s/search?term=%s&media=audiobook&country=%s&limit=20",
		it.http.BaseURL, url.QueryEscape(query), url.QueryEscape(region))
	body, err := it.http.GetJSON(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	var envelope itunesEnvelope
	if err := UnmarshalInto(body, &envelope); err != nil {
		return nil, err
	}
	out := make([]metadata.Candidate, 0, len(envelope.Results))
	for _, r := range envelope.Results {
		out = append(out, r.toCandidate(region, nil))
	}
	return out, nil
}

// itunesEnvelope is the top-level iTunes API response wrapper.
type itunesEnvelope struct {
	ResultCount int            `json:"resultCount"`
	Results     []itunesResult `json:"results"`
}

// itunesResult is a single item from the iTunes API response.
// trackTimeMillis can be very large (int64-range) so it is declared int64.
type itunesResult struct {
	TrackID          int64  `json:"trackId"`
	CollectionID     int64  `json:"collectionId"`
	TrackName        string `json:"trackName"`
	CollectionName   string `json:"collectionName"`
	ArtistName       string `json:"artistName"`
	Description      string `json:"description"`
	LongDescription  string `json:"longDescription"`
	ArtworkUrl600    string `json:"artworkUrl600"`
	ArtworkUrl100    string `json:"artworkUrl100"`
	ReleaseDate      string `json:"releaseDate"`
	TrackTimeMillis  int64  `json:"trackTimeMillis"`
	PrimaryGenreName string `json:"primaryGenreName"`
	Country          string `json:"country"`
}

func (r itunesResult) toCandidate(region string, raw []byte) metadata.Candidate {
	// Prefer trackId; fall back to collectionId.
	id := r.TrackID
	if id == 0 {
		id = r.CollectionID
	}

	// Prefer trackName; fall back to collectionName.
	title := r.TrackName
	if title == "" {
		title = r.CollectionName
	}

	// Prefer longDescription; fall back to description.
	description := r.LongDescription
	if description == "" {
		description = r.Description
	}

	// Prefer artworkUrl600; fall back to artworkUrl100.
	cover := r.ArtworkUrl600
	if cover == "" {
		cover = r.ArtworkUrl100
	}

	// Truncate releaseDate to YYYY-MM-DD.
	publishedAt := r.ReleaseDate
	if len(publishedAt) > 10 {
		publishedAt = publishedAt[:10]
	}

	// Convert milliseconds to whole minutes.
	runtimeMin := int(r.TrackTimeMillis / 60000)

	var genres []string
	if r.PrimaryGenreName != "" {
		genres = []string{r.PrimaryGenreName}
	}

	// Use the caller-supplied region for consistency (iTunes echoes "USA"
	// rather than the ISO code we sent, so we preserve our own input).
	c := metadata.Candidate{
		Source:      itunesID,
		ExternalID:  strconv.FormatInt(id, 10),
		Title:       title,
		Description: description,
		CoverURL:    cover,
		PublishedAt: publishedAt,
		RuntimeMin:  runtimeMin,
		Genres:      genres,
		Region:      region,
		Raw:         raw,
	}
	if r.ArtistName != "" {
		c.Authors = []string{r.ArtistName}
	}

	// Normalise the country echo: if iTunes returns a 3-letter code (e.g. "USA")
	// and the region we sent matches its prefix (e.g. "us"), keep our input.
	// For unexpected values we fall back to the raw echo lower-cased.
	if c.Region == "" {
		c.Region = strings.ToLower(r.Country)
	}
	return c
}
