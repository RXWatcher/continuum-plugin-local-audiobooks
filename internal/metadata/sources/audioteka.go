package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/metadata"
)

const audiotekaID = "audioteka"
const audiotekaBaseURL = "https://audioteka.com"

// audiotekaRegions are the domains Audioteka operates under.
// Typically pl and cz; the TS reference lists de/es/lt as well.
var audiotekaRegionDomains = map[string]string{
	"pl": "https://audioteka.com/pl",
	"cz": "https://audioteka.com/cz",
	"de": "https://audioteka.com/de",
	"es": "https://audioteka.com/es",
	"lt": "https://audioteka.com/lt",
}

// audiotekaSearchPaths maps each region to its localised search path segment.
// The query parameter name is "query" across all regions (per booklore-ng TS reference).
var audiotekaSearchPaths = map[string]string{
	"pl": "szukaj",
	"cz": "hledat",
	"de": "suchen",
	"es": "buscar",
	"lt": "ieškoti",
}

// audiotekaInitialStateRE matches the window.__INITIAL_STATE__ or
// window.__PRELOADED_STATE__ JSON blob embedded in Audioteka HTML pages.
var audiotekaInitialStateRE = regexp.MustCompile(
	`(?i)window\.__(?:INITIAL|PRELOADED)_STATE__\s*=\s*(\{[\s\S]*?\});`)

// Audioteka is the Source impl for Audioteka (HTML scraping via JSON-LD /
// window.__INITIAL_STATE__).
type Audioteka struct {
	http    *HTTPClient
	baseURL string // overridden in tests; empty means use region routing
}

// NewAudioteka constructs the source with the production base URL.
func NewAudioteka(ua string) *Audioteka {
	return NewAudiotekaAt(audiotekaBaseURL, ua)
}

// NewAudiotekaAt constructs the source against a custom base URL (tests).
func NewAudiotekaAt(baseURL, ua string) *Audioteka {
	return &Audioteka{
		http:    NewHTTPClient(baseURL, ua),
		baseURL: baseURL,
	}
}

func (a *Audioteka) ID() string                       { return audiotekaID }
func (a *Audioteka) Enabled(cfg map[string]bool) bool { return cfg[audiotekaID] }

// audiotekaHostFor returns the root URL to use for the given region.
// When baseURL is overridden (tests), it is used as-is.
func (a *Audioteka) audiotekaHostFor(region string) string {
	if a.baseURL != audiotekaBaseURL {
		return a.baseURL
	}
	if host, ok := audiotekaRegionDomains[region]; ok {
		return host
	}
	return audiotekaRegionDomains["pl"]
}

// Get fetches a single book by Audioteka slug.
// Returns (nil, nil) for ASIN-shaped input — Audioteka does not index by ASIN.
func (a *Audioteka) Get(ctx context.Context, externalID, region string) (*metadata.Candidate, error) {
	if asinRE.MatchString(externalID) {
		return nil, nil
	}
	if strings.TrimSpace(externalID) == "" {
		return nil, nil
	}

	host := a.audiotekaHostFor(region)
	bookURL := fmt.Sprintf("%s/audiobook/%s", host, url.PathEscape(externalID))

	body, err := a.http.GetJSON(ctx, bookURL)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	book := parseAudiotekaBookPage(body)
	if book == nil {
		return nil, ErrNotFound
	}
	if book.ExternalID == "" {
		book.ExternalID = externalID
	}
	book.Source = audiotekaID
	book.Region = region
	book.Raw = json.RawMessage(body)
	return book, nil
}

// Search queries Audioteka for audiobooks matching the given text.
// Returns (nil, nil) for ASIN-shaped queries — Audioteka does not index by ASIN.
func (a *Audioteka) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	if asinRE.MatchString(query) {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	host := a.audiotekaHostFor(region)
	searchPath := audiotekaSearchPaths[region]
	if searchPath == "" {
		searchPath = "szukaj"
	}
	searchURL := fmt.Sprintf("%s/%s?query=%s", host, searchPath, url.QueryEscape(q))

	body, err := a.http.GetJSON(ctx, searchURL)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	books := parseAudiotekaSearchPage(body)
	out := make([]metadata.Candidate, 0, len(books))
	for i := range books {
		books[i].Source = audiotekaID
		books[i].Region = region
		if books[i].Raw == nil {
			books[i].Raw = json.RawMessage(body)
		}
		out = append(out, books[i])
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// HTML page parsers
// ---------------------------------------------------------------------------

// parseAudiotekaBookPage extracts a Candidate from an HTML page body.
// Tries JSON-LD first, then window.__INITIAL_STATE__.
func parseAudiotekaBookPage(html []byte) *metadata.Candidate {
	s := string(html)

	// 1. Try JSON-LD structured data (reuse Storytel's parser — same package).
	if c := parseJSONLD(s); c != nil {
		return c
	}

	// 2. Try window.__INITIAL_STATE__ / __PRELOADED_STATE__.
	books := extractFromAudiotekaInitialState(s)
	if len(books) > 0 {
		return &books[0]
	}
	return nil
}

// parseAudiotekaSearchPage extracts Candidates from a search-result HTML page.
func parseAudiotekaSearchPage(html []byte) []metadata.Candidate {
	s := string(html)

	// Prefer __INITIAL_STATE__ which contains structured book objects.
	books := extractFromAudiotekaInitialState(s)
	if len(books) > 0 {
		if len(books) > 20 {
			books = books[:20]
		}
		return books
	}

	// Fallback: try JSON-LD ItemList or individual Audiobook entries.
	if c := parseJSONLD(s); c != nil {
		return []metadata.Candidate{*c}
	}
	return nil
}

// ---------------------------------------------------------------------------
// window.__INITIAL_STATE__ parser (Audioteka-specific)
// ---------------------------------------------------------------------------

func extractFromAudiotekaInitialState(html string) []metadata.Candidate {
	m := audiotekaInitialStateRE.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil
	}
	var data interface{}
	if err := json.Unmarshal([]byte(m[1]), &data); err != nil {
		return nil
	}
	var results []metadata.Candidate
	traverseAudiotekaData(data, &results, 0)
	return results
}

// depth bounds recursion over attacker-influenced scraped JSON (JSON-bomb /
// stack-exhaustion guard).
func traverseAudiotekaData(v interface{}, out *[]metadata.Candidate, depth int) {
	if depth > maxTraverseDepth {
		return
	}
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			traverseAudiotekaData(item, out, depth+1)
		}
	case map[string]interface{}:
		if isAudiotekaBook(val) {
			if c := audiotekaMapToCandidate(val); c != nil {
				*out = append(*out, *c)
			}
			return // don't recurse into books we already consumed
		}
		for _, child := range val {
			traverseAudiotekaData(child, out, depth+1)
		}
	}
}

// isAudiotekaBook returns true when a map looks like an Audioteka book record.
func isAudiotekaBook(m map[string]interface{}) bool {
	_, hasTitle := m["title"]
	if !hasTitle {
		return false
	}
	_, hasID := m["id"]
	_, hasAuthors := m["authors"]
	_, hasNarrators := m["narrators"]
	_, hasDuration := m["duration"]
	_, hasCoverURL := m["coverUrl"]
	_, hasISBN := m["isbn"]
	return hasID || hasAuthors || hasNarrators || hasDuration || hasCoverURL || hasISBN
}

// audiotekaMapToCandidate converts a map[string]interface{} book record to a Candidate.
// Duration from Audioteka is in seconds; we convert to minutes.
func audiotekaMapToCandidate(m map[string]interface{}) *metadata.Candidate {
	c := &metadata.Candidate{}

	c.Title = stringField(m, "title")
	if c.Title == "" {
		return nil
	}

	c.ExternalID = stringField(m, "id")
	c.Description = stringField(m, "description")
	c.Language = stringField(m, "language")
	c.ISBN = stringField(m, "isbn")
	c.Publisher = stringField(m, "publisher")

	// releaseDate
	rd := stringField(m, "releaseDate")
	if len(rd) > 10 {
		rd = rd[:10]
	}
	c.PublishedAt = rd

	// duration in seconds → minutes
	if dur, ok := m["duration"].(float64); ok && dur > 0 {
		c.RuntimeMin = int(dur) / 60
	}

	// cover: plain URL string
	c.CoverURL = stringField(m, "coverUrl")

	// authors: [{name: "..."}] or ["..."]
	if arr, ok := m["authors"].([]interface{}); ok {
		for _, a := range arr {
			switch v := a.(type) {
			case map[string]interface{}:
				if name := stringField(v, "name"); name != "" {
					c.Authors = append(c.Authors, name)
				}
			case string:
				if v != "" {
					c.Authors = append(c.Authors, v)
				}
			}
		}
	}

	// narrators: [{name: "..."}] or ["..."]
	if arr, ok := m["narrators"].([]interface{}); ok {
		for _, n := range arr {
			switch v := n.(type) {
			case map[string]interface{}:
				if name := stringField(v, "name"); name != "" {
					c.Narrators = append(c.Narrators, name)
				}
			case string:
				if v != "" {
					c.Narrators = append(c.Narrators, v)
				}
			}
		}
	}

	// series: {name: "...", orderInSeries: N}
	if ser, ok := m["series"].(map[string]interface{}); ok {
		c.Series = stringField(ser, "name")
		if pos, ok := ser["orderInSeries"].(float64); ok {
			c.SeriesPos = strconv.Itoa(int(pos))
		}
	}

	// categories / genres → []string
	for _, key := range []string{"categories", "genres", "kategorie"} {
		if cats, ok := m[key].([]interface{}); ok {
			for _, cat := range cats {
				if s, ok := cat.(string); ok && s != "" {
					c.Genres = append(c.Genres, s)
				}
			}
		}
	}

	return c
}
