package metadata

import "testing"

func TestConfidence_ExactTitle(t *testing.T) {
	c := Candidate{Title: "Project Hail Mary"}
	score := CalculateConfidence("Project Hail Mary", c, nil)
	// title 30 + completeness ~1 (1/9 * 5)
	if score < 30 || score > 31 {
		t.Errorf("expected 30-31, got %d", score)
	}
}

func TestConfidence_ASINBeatsEverythingElse(t *testing.T) {
	orig := &Candidate{ASIN: "B08G9PRS1K"}
	asinMatch := Candidate{Title: "Different Title", ASIN: "B08G9PRS1K"}
	noMatch := Candidate{Title: "Project Hail Mary", ASIN: "B0OTHER"}
	if a, b := CalculateConfidence("Project Hail Mary", asinMatch, orig),
		CalculateConfidence("Project Hail Mary", noMatch, orig); a <= b {
		t.Errorf("ASIN-match (%d) should beat title-only (%d)", a, b)
	}
}

func TestConfidence_AuthorFractional(t *testing.T) {
	orig := &Candidate{Authors: []string{"Andy Weir", "Ghost Writer"}}
	c := Candidate{Title: "X", Authors: []string{"Andy Weir"}}
	score := CalculateConfidence("X", c, orig)
	// title 30 (exact match "X" == "X"), author 15 * (1/2) = 7,
	// completeness 2/9 → 1. Total 38.
	if score != 38 {
		t.Errorf("expected 38, got %d", score)
	}
}

func TestConfidence_MissingOriginalSkipsSignals(t *testing.T) {
	c := Candidate{Title: "X", Authors: []string{"A"}, ASIN: "B00", ISBN: "9780"}
	score := CalculateConfidence("X", c, nil)
	// title 30; no ASIN/ISBN/author/narrator points without original;
	// completeness 3/9 → 1. Total 31.
	if score != 31 {
		t.Errorf("expected 31, got %d", score)
	}
}

func TestConfidence_CapsAt100(t *testing.T) {
	orig := &Candidate{
		ASIN: "B0", ISBN: "9780", Authors: []string{"A"}, Narrators: []string{"N"},
	}
	c := Candidate{
		Title: "FullMatch", ASIN: "B0", ISBN: "9780",
		Authors: []string{"A"}, Narrators: []string{"N"},
		Description: "d", CoverURL: "c", Publisher: "p", Language: "en", RuntimeMin: 100,
	}
	score := CalculateConfidence("FullMatch", c, orig)
	if score > 100 {
		t.Errorf("score must cap at 100, got %d", score)
	}
}
