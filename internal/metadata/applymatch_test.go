package metadata

import "testing"

func TestApplyMatch_OverwritesNonEmpty(t *testing.T) {
	row := AudiobookRow{Title: "old", Author: "OldA"}
	c := Candidate{Title: "new", Authors: []string{"NewA"}}
	got := ApplyMatch(row, c)
	if got.Title != "new" {
		t.Errorf("title %q", got.Title)
	}
	if got.Author != "NewA" {
		t.Errorf("author %q", got.Author)
	}
}

func TestApplyMatch_PreservesNonEmptyOnEmpty(t *testing.T) {
	row := AudiobookRow{Title: "old", Author: "OldA"}
	c := Candidate{Title: "new"}
	got := ApplyMatch(row, c)
	if got.Title != "new" {
		t.Errorf("title %q", got.Title)
	}
	if got.Author != "OldA" {
		t.Errorf("author should be preserved, got %q", got.Author)
	}
}

func TestApplyMatch_PreservesID(t *testing.T) {
	row := AudiobookRow{ID: "abc-123", Title: "old"}
	c := Candidate{Title: "new"}
	got := ApplyMatch(row, c)
	if got.ID != "abc-123" {
		t.Errorf("ID must be preserved, got %q", got.ID)
	}
}

func TestApplyMatch_RuntimeConversion(t *testing.T) {
	row := AudiobookRow{}
	c := Candidate{RuntimeMin: 100}
	got := ApplyMatch(row, c)
	if got.DurationMS != 6_000_000 {
		t.Errorf("expected 6_000_000 ms, got %d", got.DurationMS)
	}
}

func TestApplyMatch_YearExtract(t *testing.T) {
	row := AudiobookRow{}
	got := ApplyMatch(row, Candidate{PublishedAt: "2021-05-04"})
	if got.Year != "2021" {
		t.Errorf("year %q", got.Year)
	}
	got = ApplyMatch(row, Candidate{PublishedAt: "2021"})
	if got.Year != "2021" {
		t.Errorf("year-only %q", got.Year)
	}
	got = ApplyMatch(row, Candidate{PublishedAt: ""})
	if got.Year != "" {
		t.Errorf("empty year %q", got.Year)
	}
}
