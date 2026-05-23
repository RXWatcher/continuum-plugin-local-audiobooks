package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/metadata"
)

const bookbeatID = "bookbeat"
const bookbeatBaseURL = "https://www.bookbeat.com"

// regionDomains maps BookBeat region codes to their base domains.
// Derived from the reference .ts file (REGION_DOMAINS map).
var bookbeatRegionDomains = map[string]string{
	"se": "https://www.bookbeat.com/se",
	"fi": "https://www.bookbeat.com/fi",
	"de": "https://www.bookbeat.de",
	"at": "https://www.bookbeat.at",
	"ch": "https://www.bookbeat.ch",
	"dk": "https://www.bookbeat.com/dk",
	"no": "https://www.bookbeat.com/no",
	"pl": "https://www.bookbeat.com/pl",
	"nl": "https://www.bookbeat.com/nl",
	"uk": "https://www.bookbeat.com/uk",
}

// bookbeatBookPaths are the path prefixes BookBeat uses per language for book
// detail pages. We try them in order until one succeeds.
var bookbeatBookPaths = []string{"book", "bok", "buch", "boek"}

// BookBeat is the Source impl for BookBeat (HTML scraping via JSON-LD / __NEXT_DATA__).
type BookBeat struct {
	http    *HTTPClient
	baseURL string // overridden in tests; empty means use region routing
}

// NewBookBeat constructs the source with production base URL routing.
func NewBookBeat(ua string) *BookBeat {
	return NewBookBeatAt(bookbeatBaseURL, ua)
}

// NewBookBeatAt constructs the source against a custom base URL (tests).
func NewBookBeatAt(baseURL, ua string) *BookBeat {
	return &BookBeat{
		http:    NewHTTPClient(baseURL, ua),
		baseURL: baseURL,
	}
}

func (b *BookBeat) ID() string                       { return bookbeatID }
func (b *BookBeat) Enabled(cfg map[string]bool) bool { return cfg[bookbeatID] }

// bookbeatHostFor returns the root URL to use for the given region.
// When baseURL is overridden (tests), it is used as-is.
func (b *BookBeat) bookbeatHostFor(region string) string {
	if b.baseURL != bookbeatBaseURL {
		return b.baseURL
	}
	if host, ok := bookbeatRegionDomains[region]; ok {
		return host
	}
	return bookbeatRegionDomains["se"]
}

// Get fetches a single book by BookBeat slug.
// Returns (nil, nil) for ASIN-shaped input — BookBeat does not index by ASIN.
func (b *BookBeat) Get(ctx context.Context, externalID, region string) (*metadata.Candidate, error) {
	if asinRE.MatchString(externalID) {
		return nil, nil
	}
	if strings.TrimSpace(externalID) == "" {
		return nil, nil
	}

	host := b.bookbeatHostFor(region)

	// Try each known book-path prefix until one responds with a parseable page.
	var lastErr error
	for _, path := range bookbeatBookPaths {
		bookURL := fmt.Sprintf("%s/%s/%s", host, path, url.PathEscape(externalID))
		body, err := b.http.GetJSON(ctx, bookURL)
		if errors.Is(err, ErrNotFound) {
			lastErr = err
			continue
		}
		if err != nil {
			return nil, err
		}
		book := parseBookBeatBookPage(body)
		if book == nil {
			lastErr = ErrNotFound
			continue
		}
		if book.ExternalID == "" {
			book.ExternalID = externalID
		}
		book.Source = bookbeatID
		book.Region = region
		book.Raw = json.RawMessage(body)
		return book, nil
	}

	if errors.Is(lastErr, ErrNotFound) {
		return nil, ErrNotFound
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNotFound
}

// Search queries BookBeat for audiobooks matching the given text.
// Returns (nil, nil) for ASIN-shaped queries — BookBeat does not index by ASIN.
func (b *BookBeat) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	if asinRE.MatchString(query) {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	host := b.bookbeatHostFor(region)
	searchURL := fmt.Sprintf("%s/search?q=%s", host, url.QueryEscape(q))

	body, err := b.http.GetJSON(ctx, searchURL)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	books := parseBookBeatSearchPage(body)
	out := make([]metadata.Candidate, 0, len(books))
	for i := range books {
		books[i].Source = bookbeatID
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

// parseBookBeatBookPage extracts a Candidate from an HTML page body.
// Tries JSON-LD first, then __NEXT_DATA__.
func parseBookBeatBookPage(html []byte) *metadata.Candidate {
	s := string(html)

	// 1. Try JSON-LD structured data (reuse Storytel's parser — same package).
	if c := parseJSONLD(s); c != nil {
		return c
	}

	// 2. Try __NEXT_DATA__ embedded JSON.
	books := extractFromBookBeatNextData(s)
	if len(books) > 0 {
		return &books[0]
	}
	return nil
}

// parseBookBeatSearchPage extracts Candidates from a search-result HTML page.
func parseBookBeatSearchPage(html []byte) []metadata.Candidate {
	s := string(html)

	// Prefer __NEXT_DATA__ which contains structured book objects.
	books := extractFromBookBeatNextData(s)
	if len(books) > 0 {
		if len(books) > 20 {
			books = books[:20]
		}
		return books
	}
	return nil
}

// ---------------------------------------------------------------------------
// __NEXT_DATA__ parser (BookBeat-specific field names)
// ---------------------------------------------------------------------------

func extractFromBookBeatNextData(html string) []metadata.Candidate {
	m := nextDataRE.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil
	}
	var data interface{}
	if err := json.Unmarshal([]byte(m[1]), &data); err != nil {
		return nil
	}
	var results []metadata.Candidate
	traverseBookBeatNextData(data, &results, 0)
	return results
}

// depth bounds recursion over attacker-influenced scraped JSON (JSON-bomb /
// stack-exhaustion guard).
func traverseBookBeatNextData(v interface{}, out *[]metadata.Candidate, depth int) {
	if depth > maxTraverseDepth {
		return
	}
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			traverseBookBeatNextData(item, out, depth+1)
		}
	case map[string]interface{}:
		if isBookBeatBook(val) {
			if c := bookbeatMapToCandidate(val); c != nil {
				*out = append(*out, *c)
			}
			return // don't recurse into books we already consumed
		}
		for _, child := range val {
			traverseBookBeatNextData(child, out, depth+1)
		}
	}
}

// isBookBeatBook returns true when a map looks like a BookBeat book record.
func isBookBeatBook(m map[string]interface{}) bool {
	_, hasTitle := m["title"]
	if !hasTitle {
		return false
	}
	_, hasID := m["id"]
	_, hasBookID := m["bookId"]
	_, hasAuthors := m["authors"]
	_, hasNarrators := m["narrators"]
	_, hasDuration := m["duration"]
	_, hasCoverURL := m["coverUrl"]
	return hasID || hasBookID || hasAuthors || hasNarrators || hasDuration || hasCoverURL
}

// bookbeatMapToCandidate converts a map[string]interface{} book record to a Candidate.
func bookbeatMapToCandidate(m map[string]interface{}) *metadata.Candidate {
	c := &metadata.Candidate{}

	c.Title = stringField(m, "title")
	if c.Title == "" {
		return nil
	}

	// ExternalID: prefer explicit id fields
	c.ExternalID = stringField(m, "id")
	if c.ExternalID == "" {
		c.ExternalID = stringField(m, "bookId")
	}

	c.Description = stringField(m, "description")
	c.Language = stringField(m, "language")
	c.ISBN = stringField(m, "isbn")
	c.Publisher = stringField(m, "publisher")

	// releaseDate / publishDate
	rd := stringField(m, "releaseDate")
	if rd == "" {
		rd = stringField(m, "publishDate")
	}
	if len(rd) > 10 {
		rd = rd[:10]
	}
	c.PublishedAt = rd

	// duration in seconds → minutes
	if dur, ok := m["duration"].(float64); ok && dur > 0 {
		c.RuntimeMin = int(dur) / 60
	}

	// cover: BookBeat embeds a plain URL string in "coverUrl"
	c.CoverURL = stringField(m, "coverUrl")
	// Also handle nested cover object (same shape as Storytel) as fallback
	if c.CoverURL == "" {
		if cover, ok := m["cover"].(map[string]interface{}); ok {
			if sizes, ok := cover["sizes"].([]interface{}); ok && len(sizes) > 0 {
				bestURL := ""
				bestWidth := -1
				for _, sz := range sizes {
					if s, ok := sz.(map[string]interface{}); ok {
						if wf, ok := s["width"].(float64); ok {
							w := int(wf)
							if w > bestWidth {
								bestWidth = w
								bestURL = stringField(s, "url")
							}
						}
					}
				}
				c.CoverURL = bestURL
			}
			if c.CoverURL == "" {
				c.CoverURL = stringField(cover, "url")
			}
		}
	}

	// authors
	if arr, ok := m["authors"].([]interface{}); ok {
		for _, a := range arr {
			if obj, ok := a.(map[string]interface{}); ok {
				if name := stringField(obj, "name"); name != "" {
					c.Authors = append(c.Authors, name)
				}
			}
		}
	}

	// narrators
	if arr, ok := m["narrators"].([]interface{}); ok {
		for _, n := range arr {
			if obj, ok := n.(map[string]interface{}); ok {
				if name := stringField(obj, "name"); name != "" {
					c.Narrators = append(c.Narrators, name)
				}
			}
		}
	}

	// series
	if ser, ok := m["series"].(map[string]interface{}); ok {
		c.Series = stringField(ser, "name")
		if pos, ok := ser["orderInSeries"].(float64); ok {
			c.SeriesPos = strconv.Itoa(int(pos))
		}
	}

	// categories → genres
	if cats, ok := m["categories"].([]interface{}); ok {
		for _, cat := range cats {
			if s, ok := cat.(string); ok && s != "" {
				c.Genres = append(c.Genres, s)
			}
		}
	}

	return c
}
