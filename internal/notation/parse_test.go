package notation

import (
	"math"
	"strings"
	"testing"
)

func TestParseReadsHeaderAndNotes(t *testing.T) {
	text := "clip \"Chorus Lead\" bars=4 sig=4/4\n\n" +
		"  1:1 C3 1/8 v100\n" +
		"  2:3 A#3 1/8 v88 -\n"

	c, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if c.Name != "Chorus Lead" || c.Bars != 4 || c.SigNum != 4 || c.SigDen != 4 {
		t.Errorf("header = %+v", c)
	}
	if len(c.Notes) != 2 {
		t.Fatalf("got %d notes, want 2", len(c.Notes))
	}
	if c.Notes[0] != (Note{Pitch: 60, StartTime: 0, Duration: 0.5, Velocity: 100}) {
		t.Errorf("first note = %+v", c.Notes[0])
	}
	if c.Notes[1] != (Note{Pitch: 70, StartTime: 6, Duration: 0.5, Velocity: 88, Mute: true}) {
		t.Errorf("second note = %+v", c.Notes[1])
	}
}

func TestParseToleratesWhitespaceAndComments(t *testing.T) {
	// Hand-aligned columns and whole-line comments must both read fine.
	text := "clip \"Pad\"   bars=2   sig=4/4\n" +
		"# a comment line\n" +
		"\n" +
		"   1:1     C3    1/4   v90\n"
	c, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if len(c.Notes) != 1 || c.Notes[0].Pitch != 60 {
		t.Errorf("notes = %+v", c.Notes)
	}
}

func TestParseKeepsSharpsOutOfComments(t *testing.T) {
	// A '#' only starts a comment at the beginning of a line. Treating it as a
	// comment anywhere would eat every sharp note name.
	c, err := Parse("clip \"x\" bars=1 sig=4/4\n  1:1 C#3 1/4 v90\n")
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if len(c.Notes) != 1 || c.Notes[0].Pitch != 61 {
		t.Errorf("notes = %+v", c.Notes)
	}
}

func TestParseNonFourFour(t *testing.T) {
	c, err := Parse("clip \"x\" bars=2 sig=6/8\n  2:1 C3 1/8 v90\n")
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	// 6/8 is three beats per bar, so bar two starts at beat three.
	if math.Abs(c.Notes[0].StartTime-3) > 1e-9 {
		t.Errorf("start time = %v, want 3", c.Notes[0].StartTime)
	}
}

func TestParseErrorsCarryLineNumbers(t *testing.T) {
	_, err := Parse("clip \"x\" bars=1 sig=4/4\n  1:1 C3 1/4 v90\n  bogus line here\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error should name the line, got %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	bad := []string{
		"",
		"  1:1 C3 1/4 v90\n",
		"clip x bars=1 sig=4/4\n",
		"clip \"x\" sig=4/4\n",
		"clip \"x\" bars=1\n",
		"clip \"x\" bars=1 sig=4/4\n  1:1 Eb3 1/4 v90\n",
		"clip \"x\" bars=1 sig=4/4\n  1:1 C3 1/4 100\n",
		"clip \"x\" bars=1 sig=4/4\n  1:1 C3 1/4 v90 x\n",
		"clip \"x\" bars=1 sig=4/4\n  1:1 C3 1/4 v0\n",
	}
	for i, text := range bad {
		if _, err := Parse(text); err == nil {
			t.Errorf("case %d: expected an error for %q", i, text)
		}
	}
}
