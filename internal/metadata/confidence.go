package metadata

import "strings"

// confidence weights: ASIN 35, title 30, author 15, ISBN 15,
// narrator 10, completeness 5. Total may exceed 100; capped at 100.
// ASIN outweighs title so that a hard identifier match beats a fuzzy
// title match (see TestConfidence_ASINBeatsEverythingElse).

// CalculateConfidence scores `candidate` against `query` (text) and an
// optional `original` (the audiobook's current metadata, used for
// identifier comparisons). Missing inputs are skipped, not penalized;
// the score's effective ceiling drops accordingly.
func CalculateConfidence(query string, candidate Candidate, original *Candidate) int {
	score := 0

	// Title (30) - based on similarity to query string
	if candidate.Title != "" && query != "" {
		score += titleScore(query, candidate.Title)
	}

	// ASIN exact match (35)
	if original != nil && original.ASIN != "" && candidate.ASIN != "" {
		if strings.EqualFold(original.ASIN, candidate.ASIN) {
			score += 35
		}
	}

	// Author match (15) - fractional, based on original
	if original != nil && len(original.Authors) > 0 && len(candidate.Authors) > 0 {
		score += int(float64(matchingNames(original.Authors, candidate.Authors)) /
			float64(len(original.Authors)) * 15.0)
	}

	// ISBN exact match (15)
	if original != nil && original.ISBN != "" && candidate.ISBN != "" {
		if original.ISBN == candidate.ISBN {
			score += 15
		}
	}

	// Narrator match (10) - fractional, based on original
	if original != nil && len(original.Narrators) > 0 && len(candidate.Narrators) > 0 {
		score += int(float64(matchingNames(original.Narrators, candidate.Narrators)) /
			float64(len(original.Narrators)) * 10.0)
	}

	// Completeness (5) - fraction of the 9-field set populated
	score += completenessScore(candidate)

	if score > 100 {
		return 100
	}
	return score
}

// titleScore: 30 for case-insensitive equality, 22 for substring containment
// either direction, otherwise proportional to overlapping whitespace-split words.
func titleScore(query, title string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	t := strings.ToLower(strings.TrimSpace(title))
	if q == t {
		return 30
	}
	if strings.Contains(t, q) || strings.Contains(q, t) {
		return 22
	}
	qw := strings.Fields(q)
	tw := strings.Fields(t)
	if len(qw) == 0 {
		return 0
	}
	matched := 0
	for _, w := range qw {
		for _, tword := range tw {
			if strings.Contains(tword, w) || strings.Contains(w, tword) {
				matched++
				break
			}
		}
	}
	return int(float64(matched) / float64(len(qw)) * 30.0)
}

// matchingNames returns the count of names from `original` that match
// (substring either direction, case-insensitive) at least one name in `candidates`.
func matchingNames(original, candidates []string) int {
	n := 0
	for _, o := range original {
		ol := strings.ToLower(strings.TrimSpace(o))
		if ol == "" {
			continue
		}
		for _, c := range candidates {
			cl := strings.ToLower(strings.TrimSpace(c))
			if cl == "" {
				continue
			}
			if strings.Contains(cl, ol) || strings.Contains(ol, cl) {
				n++
				break
			}
		}
	}
	return n
}

// completenessScore awards up to 5 points based on field population.
// 9 fields counted: title, authors, narrators, description, cover_url,
// (asin OR isbn), publisher, language, runtime_min.
func completenessScore(c Candidate) int {
	fields := 0
	if c.Title != "" {
		fields++
	}
	if len(c.Authors) > 0 {
		fields++
	}
	if len(c.Narrators) > 0 {
		fields++
	}
	if c.Description != "" {
		fields++
	}
	if c.CoverURL != "" {
		fields++
	}
	if c.ASIN != "" || c.ISBN != "" {
		fields++
	}
	if c.Publisher != "" {
		fields++
	}
	if c.Language != "" {
		fields++
	}
	if c.RuntimeMin > 0 {
		fields++
	}
	return int(float64(fields) / 9.0 * 5.0)
}
