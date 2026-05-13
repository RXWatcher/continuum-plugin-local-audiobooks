package metadata

import "testing"

func TestFormatExternalID(t *testing.T) {
	got := FormatExternalID("audnexus", "B0EXAMPLE")
	if got != "audnexus:B0EXAMPLE" {
		t.Fatalf("got %q", got)
	}
}

func TestParseExternalID(t *testing.T) {
	cases := []struct {
		in         string
		wantSource string
		wantID     string
		wantErr    bool
	}{
		{"audnexus:B0EXAMPLE", "audnexus", "B0EXAMPLE", false},
		{"storytel:abc-xyz-123", "storytel", "abc-xyz-123", false},
		{"itunes:1234567890", "itunes", "1234567890", false},
		{"", "", "", true},
		{"noprefix", "", "", true},
		{":missingsource", "", "", true},
		{"missingid:", "", "", true},
	}
	for _, c := range cases {
		src, id, err := ParseExternalID(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("Parse(%q) err = %v, wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if src != c.wantSource || id != c.wantID {
			t.Errorf("Parse(%q) = (%q,%q), want (%q,%q)", c.in, src, id, c.wantSource, c.wantID)
		}
	}
}
