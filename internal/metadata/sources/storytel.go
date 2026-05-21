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

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/metadata"
)

const storytelID = "storytel"
const storytelBaseURL = "https://www.storytel.com"

// storytelBookIDRE matches Storytel native IDs: slugs like "project-hail-mary-12345"
// or pure numeric IDs. We accept any non-empty string that is not an ASIN.
var storytelBookIDRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9\-_]*$`)

// Storytel is the Source impl for Storytel (web scraping via JSON-LD / __NEXT_DATA__).
type Storytel struct {
	http    *HTTPClient
	baseURL string // overridden in tests; empty means use storytelBaseURL
}

// NewStorytel constructs the source with the production base URL.
func NewStorytel(ua string) *Storytel {
	return NewStorytelAt(storytelBaseURL, ua)
}

// NewStorytelAt constructs the source against a custom base URL (tests).
func NewStorytelAt(baseURL, ua string) *Storytel {
	return &Storytel{
		http:    NewHTTPClient(baseURL, ua),
		baseURL: baseURL,
	}
}

func (s *Storytel) ID() string                       { return storytelID }
func (s *Storytel) Enabled(cfg map[string]bool) bool { return cfg[storytelID] }

// storytelHostFor returns the host root URL for the given region.
// When baseURL is overridden (tests), it is used as-is.
func (s *Storytel) storytelHostFor(region string) string {
	if s.baseURL != storytelBaseURL {
		return s.baseURL
	}
	switch region {
	case "uk":
		return "https://www.storytel.co.uk"
	case "us", "":
		return "https://www.storytel.com"
	default:
		// region is interpolated into the request host, so only accept a
		// bare ccTLD-shaped token (2-3 lowercase letters). Anything else
		// (e.g. "com.attacker.net", "com/..") would point the request at an
		// arbitrary host — SSRF. Fall back to the global .com site.
		if !isCCTLD(region) {
			return "https://www.storytel.com"
		}
		return "https://www.storytel." + region
	}
}

func isCCTLD(s string) bool {
	if len(s) < 2 || len(s) > 3 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return true
}

// Get fetches a single book by Storytel native ID (slug or numeric ID).
// Returns (nil, nil) for ASIN-shaped input — Storytel does not index by ASIN.
func (s *Storytel) Get(ctx context.Context, externalID, region string) (*metadata.Candidate, error) {
	if asinRE.MatchString(externalID) {
		return nil, nil
	}
	if strings.TrimSpace(externalID) == "" {
		return nil, nil
	}

	host := s.storytelHostFor(region)
	bookURL := fmt.Sprintf("%s/books/%s", host, url.PathEscape(externalID))

	body, err := s.http.GetJSON(ctx, bookURL)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	book := parseStorytelBookPage(body)
	if book == nil {
		return nil, ErrNotFound
	}
	// Ensure the ExternalID is set — the page parser may not have it.
	if book.ExternalID == "" {
		book.ExternalID = externalID
	}
	book.Source = storytelID
	book.Region = region
	book.Raw = json.RawMessage(body)
	return book, nil
}

// Search queries Storytel for audiobooks matching the given text.
// Returns (nil, nil) for ASIN-shaped queries — Storytel does not index by ASIN.
func (s *Storytel) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	if asinRE.MatchString(query) {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	host := s.storytelHostFor(region)
	searchURL := fmt.Sprintf("%s/search?query=%s", host, url.QueryEscape(q))

	body, err := s.http.GetJSON(ctx, searchURL)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	books := parseStorytelSearchPage(body)
	out := make([]metadata.Candidate, 0, len(books))
	for i := range books {
		books[i].Source = storytelID
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

// parseStorytelBookPage extracts a Candidate from an HTML page body.
// It tries JSON-LD first, then __NEXT_DATA__.
func parseStorytelBookPage(html []byte) *metadata.Candidate {
	s := string(html)

	// 1. Try JSON-LD structured data.
	if c := parseJSONLD(s); c != nil {
		return c
	}

	// 2. Try __NEXT_DATA__ embedded JSON.
	books := extractFromNextData(s)
	if len(books) > 0 {
		return &books[0]
	}
	return nil
}

// parseStorytelSearchPage extracts Candidates from a search-result HTML page.
func parseStorytelSearchPage(html []byte) []metadata.Candidate {
	s := string(html)

	// Prefer __NEXT_DATA__ which contains structured book objects.
	books := extractFromNextData(s)
	if len(books) > 0 {
		if len(books) > 20 {
			books = books[:20]
		}
		return books
	}
	return nil
}

// ---------------------------------------------------------------------------
// JSON-LD parser (book detail page)
// ---------------------------------------------------------------------------

var jsonLDRE = regexp.MustCompile(`(?i)<script[^>]*type="application/ld\+json"[^>]*>([\s\S]*?)</script>`)

func parseJSONLD(html string) *metadata.Candidate {
	allMatches := jsonLDRE.FindAllStringSubmatch(html, -1)
	var doc map[string]json.RawMessage
	found := false
	for _, matches := range allMatches {
		if len(matches) < 2 {
			continue
		}
		var d map[string]json.RawMessage
		if err := json.Unmarshal([]byte(matches[1]), &d); err != nil {
			continue
		}
		var typ string
		if err := json.Unmarshal(d["@type"], &typ); err != nil {
			continue
		}
		if typ == "Audiobook" || typ == "Book" {
			doc = d
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	c := &metadata.Candidate{}

	// title / name
	var name string
	if err := json.Unmarshal(doc["name"], &name); err == nil {
		c.Title = name
	}

	// description
	var desc string
	if err := json.Unmarshal(doc["description"], &desc); err == nil {
		c.Description = desc
	}

	// isbn
	var isbn string
	if err := json.Unmarshal(doc["isbn"], &isbn); err == nil {
		c.ISBN = isbn
	}

	// inLanguage
	var lang string
	if err := json.Unmarshal(doc["inLanguage"], &lang); err == nil {
		c.Language = lang
	}

	// datePublished
	var datePublished string
	if err := json.Unmarshal(doc["datePublished"], &datePublished); err == nil {
		if len(datePublished) > 10 {
			datePublished = datePublished[:10]
		}
		c.PublishedAt = datePublished
	}

	// image
	var image string
	if err := json.Unmarshal(doc["image"], &image); err == nil {
		c.CoverURL = image
	}

	// publisher
	if pub, ok := doc["publisher"]; ok {
		var pubName string
		if err := json.Unmarshal(pub, &pubName); err == nil {
			c.Publisher = pubName
		} else {
			var pubObj struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(pub, &pubObj); err == nil {
				c.Publisher = pubObj.Name
			}
		}
	}

	// author — may be object or array
	if authRaw, ok := doc["author"]; ok {
		c.Authors = extractNames(authRaw)
	}

	// readBy (narrator) — may be object or array
	if narRaw, ok := doc["readBy"]; ok {
		c.Narrators = extractNames(narRaw)
	}

	// duration — ISO 8601 duration string e.g. "PT16H11M23S"
	if durRaw, ok := doc["duration"]; ok {
		var durStr string
		if err := json.Unmarshal(durRaw, &durStr); err == nil {
			c.RuntimeMin = parseDurationToMin(durStr)
		}
	}

	// url — derive ExternalID from the path segment
	if urlRaw, ok := doc["url"]; ok {
		var pageURL string
		if err := json.Unmarshal(urlRaw, &pageURL); err == nil {
			c.ExternalID = slugFromURL(pageURL)
		}
	}

	if c.Title == "" {
		return nil
	}
	return c
}

// extractNames decodes a JSON value that may be a single {name} object
// or an array of {name} objects.
func extractNames(raw json.RawMessage) []string {
	// Try array first.
	var arr []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		names := make([]string, 0, len(arr))
		for _, a := range arr {
			if a.Name != "" {
				names = append(names, a.Name)
			}
		}
		return names
	}
	// Try single object.
	var single struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &single); err == nil && single.Name != "" {
		return []string{single.Name}
	}
	// Try plain string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
}

// parseDurationToMin converts an ISO 8601 duration like "PT16H11M23S" to minutes.
func parseDurationToMin(d string) int {
	re := regexp.MustCompile(`PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)
	m := re.FindStringSubmatch(d)
	if m == nil {
		return 0
	}
	hours, _ := strconv.Atoi(m[1])
	minutes, _ := strconv.Atoi(m[2])
	seconds, _ := strconv.Atoi(m[3])
	return hours*60 + minutes + seconds/60
}

// slugFromURL extracts the last path segment from a URL string.
func slugFromURL(u string) string {
	parts := strings.Split(strings.TrimSuffix(u, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// ---------------------------------------------------------------------------
// __NEXT_DATA__ parser (search + fallback for book pages)
// ---------------------------------------------------------------------------

var nextDataRE = regexp.MustCompile(`(?i)<script[^>]*id="__NEXT_DATA__"[^>]*>([\s\S]*?)</script>`)

func extractFromNextData(html string) []metadata.Candidate {
	m := nextDataRE.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil
	}
	var data interface{}
	if err := json.Unmarshal([]byte(m[1]), &data); err != nil {
		return nil
	}
	var results []metadata.Candidate
	traverseNextData(data, &results, 0)
	return results
}

// traverseNextData walks the arbitrary __NEXT_DATA__ structure and collects
// objects that look like StorytelBook records. depth bounds recursion: the
// payload is attacker-influenced scraped HTML, so a deeply nested JSON tree
// must not exhaust the goroutine stack.
func traverseNextData(v interface{}, out *[]metadata.Candidate, depth int) {
	if depth > maxTraverseDepth {
		return
	}
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			traverseNextData(item, out, depth+1)
		}
	case map[string]interface{}:
		if isStorytelBook(val) {
			if c := storytelMapToCandidate(val); c != nil {
				*out = append(*out, *c)
			}
			return // don't recurse into books we already consumed
		}
		for _, child := range val {
			traverseNextData(child, out, depth+1)
		}
	}
}

// isStorytelBook returns true when a map looks like a Storytel book record.
func isStorytelBook(m map[string]interface{}) bool {
	_, hasTitle := m["title"]
	if !hasTitle {
		return false
	}
	_, hasConsumable := m["consumableId"]
	_, hasBookID := m["bookId"]
	_, hasAuthors := m["authors"]
	_, hasNarrators := m["narrators"]
	_, hasDuration := m["duration"]
	return hasConsumable || hasBookID || hasAuthors || hasNarrators || hasDuration
}

// storytelMapToCandidate converts a map[string]interface{} book record to a Candidate.
func storytelMapToCandidate(m map[string]interface{}) *metadata.Candidate {
	c := &metadata.Candidate{}

	c.Title = stringField(m, "title")
	if c.Title == "" {
		return nil
	}

	// ExternalID: prefer consumableId, then bookId
	c.ExternalID = stringField(m, "consumableId")
	if c.ExternalID == "" {
		c.ExternalID = stringField(m, "bookId")
	}

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

	// cover
	if cover, ok := m["cover"].(map[string]interface{}); ok {
		// prefer largest size
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

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
