package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/metadata"
)

const audnexusID = "audnexus"
const audnexusBaseURL = "https://api.audnex.us"

var asinRE = regexp.MustCompile(`^B0[0-9A-Z]{8}$`)

// Audnexus is the Source impl for api.audnex.us.
type Audnexus struct {
	http *HTTPClient
}

// NewAudnexus constructs the source with the production base URL.
func NewAudnexus(ua string) *Audnexus {
	return NewAudnexusAt(audnexusBaseURL, ua)
}

// NewAudnexusAt constructs the source against a custom base URL (tests).
func NewAudnexusAt(baseURL, ua string) *Audnexus {
	return &Audnexus{http: NewHTTPClient(baseURL, ua)}
}

func (a *Audnexus) ID() string                       { return audnexusID }
func (a *Audnexus) Enabled(cfg map[string]bool) bool { return cfg[audnexusID] }

// Search dispatches on the shape of `query`: ASIN regex → book lookup;
// otherwise text → author search → books by first matching author, filtered
// by title overlap.
func (a *Audnexus) Search(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
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
	return a.searchByAuthor(ctx, query, region)
}

// Get looks up a single book by ASIN.
func (a *Audnexus) Get(ctx context.Context, asin, region string) (*metadata.Candidate, error) {
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
	var book audnexusBook
	if err := UnmarshalInto(body, &book); err != nil {
		return nil, err
	}

	// Best-effort chapter fetch; ignore failure.
	chURL := fmt.Sprintf("%s/books/%s/chapters?region=%s",
		a.http.BaseURL, url.PathEscape(asin), url.QueryEscape(region))
	var chapters []metadata.Chapter
	if chBody, chErr := a.http.GetJSON(ctx, chURL); chErr == nil {
		var chs audnexusChapters
		if json.Unmarshal(chBody, &chs) == nil {
			for _, c := range chs.Chapters {
				chapters = append(chapters, metadata.Chapter{
					Title:   c.Title,
					StartMS: c.StartOffsetMs,
					EndMS:   c.StartOffsetMs + c.LengthMs,
				})
			}
		}
	}

	c := book.toCandidate(region, body)
	c.Chapters = chapters
	return &c, nil
}

// searchByAuthor implements the text path: /authors?name=<first word> then
// filter by title substring across authors[].books.
func (a *Audnexus) searchByAuthor(ctx context.Context, query, region string) ([]metadata.Candidate, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	firstWord := strings.Fields(q)[0]
	authorsURL := fmt.Sprintf("%s/authors?name=%s&region=%s",
		a.http.BaseURL, url.QueryEscape(firstWord), url.QueryEscape(region))
	body, err := a.http.GetJSON(ctx, authorsURL)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var authors []audnexusAuthor
	if err := UnmarshalInto(body, &authors); err != nil {
		return nil, err
	}

	var out []metadata.Candidate
	qLower := strings.ToLower(q)
	for _, au := range authors {
		for _, b := range au.Books {
			if !strings.Contains(strings.ToLower(b.Title), qLower) &&
				!strings.Contains(qLower, strings.ToLower(b.Title)) {
				continue
			}
			full, err := a.Get(ctx, b.ASIN, region)
			if err != nil || full == nil {
				continue
			}
			out = append(out, *full)
		}
	}
	return out, nil
}

// audnexusBook is the partial shape of /books/<asin>.
type audnexusBook struct {
	ASIN             string          `json:"asin"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Image            string          `json:"image"`
	PublishedDate    string          `json:"publishedDate"`
	PublisherName    string          `json:"publisherName"`
	Language         string          `json:"language"`
	FormatType       string          `json:"formatType"`
	RuntimeLengthMin int             `json:"runtimeLengthMin"`
	Authors          []audnexusName  `json:"authors"`
	Narrators        []audnexusName  `json:"narrators"`
	Genres           []audnexusGenre `json:"genres"`
	SeriesPrimary    *struct {
		Name     string `json:"name"`
		Position string `json:"position"`
	} `json:"seriesPrimary,omitempty"`
}

type audnexusName struct {
	Name string `json:"name"`
}
type audnexusGenre struct {
	Name string `json:"name"`
}

type audnexusAuthor struct {
	ASIN  string `json:"asin"`
	Name  string `json:"name"`
	Books []struct {
		ASIN  string `json:"asin"`
		Title string `json:"title"`
	} `json:"books"`
}

type audnexusChapters struct {
	Chapters []struct {
		Title         string `json:"title"`
		StartOffsetMs int64  `json:"startOffsetMs"`
		LengthMs      int64  `json:"lengthMs"`
	} `json:"chapters"`
}

func (b audnexusBook) toCandidate(region string, raw []byte) metadata.Candidate {
	c := metadata.Candidate{
		Source:      audnexusID,
		ExternalID:  b.ASIN,
		Title:       b.Title,
		Description: b.Description,
		ASIN:        b.ASIN,
		CoverURL:    b.Image,
		PublishedAt: b.PublishedDate,
		Publisher:   b.PublisherName,
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
	if b.SeriesPrimary != nil {
		c.Series = b.SeriesPrimary.Name
		c.SeriesPos = b.SeriesPrimary.Position
	}
	return c
}
