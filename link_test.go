package main

import "testing"

func TestParseLink(t *testing.T) {
	table := []struct {
		in    string
		label string
		ok    bool
	}{
		{"((http://url-to-thing.com/path?query#fragment))", "url-to-thing.com/path", true},
		{"((http://url-to-thing.com/path?query#fragment label))", "label", true},
		{"((http://url-to-thing.com/path?query#fragment multi-word label))", "multi-word label", true},
		{"(( http://url-to-thing.com/path?query#fragment multi-word label ))", "multi-word label", true},
		{"((not-a-useful-url multi-word label))", "multi-word label", true},
		{"not a link", "multi-word label", false},
		{"(())", "", false},
		{"((\t\n\r ))", "", false},
		{"(* what are you doing this isn't applescript *)", "", false},
		{"((", "", false},
		{"))", "", false},
		{"(())))", "))", true}, // The most bizarre things are URLs.
		{"((f))", "f", true},
		{"(( f:// ))", "f://", true},
		{"((f://host))", "host", true},
		{"((f://host {}))", "{}", true},
		{"((f://host%20 {}))", "{}", false},
		{"((f://host/%20 {}))", "{}", true},
	}

	for _, e := range table {
		switch l, err := parseLink(e.in); {
		case (err == nil) != e.ok:
			t.Errorf("failed to correctly parse %q: %v\n%v\n%q", e.in, err, l.URL, l.Label)
		case err != nil && !e.ok:
			// pass -- we don't care what the label is at this point
		case l.Label != e.label:
			t.Errorf("expected label %q; got %q for %q\n%v\n%q", e.label, l.Label, e.in, l.URL, l.Label)
		}
	}
}
