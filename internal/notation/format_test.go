package notation

import "testing"

func TestFormatSortsAndSpells(t *testing.T) {
	c := Clip{
		Name:   "Chorus Lead",
		Bars:   4,
		SigNum: 4,
		SigDen: 4,
		Notes: []Note{
			// deliberately out of order: Format is what puts them in order
			{Pitch: 67, StartTime: 4, Duration: 1, Velocity: 104},
			{Pitch: 60, StartTime: 0, Duration: 0.5, Velocity: 100},
			{Pitch: 63, StartTime: 2, Duration: 0.5, Velocity: 92},
			{Pitch: 70, StartTime: 6, Duration: 0.5, Velocity: 88, Mute: true},
			{Pitch: 72, StartTime: 8.5, Duration: 0.75, Velocity: 96},
		},
	}
	want := "clip \"Chorus Lead\" bars=4 sig=4/4\n\n" +
		"  1:1 C3 1/8 v100\n" +
		"  1:3 D#3 1/8 v92\n" +
		"  2:1 G3 1/4 v104\n" +
		"  2:3 A#3 1/8 v88 -\n" +
		"  3:1.5 C4 1/8. v96\n"

	got, err := Format(c)
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	if got != want {
		t.Errorf("Format() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatSameNotesSameText(t *testing.T) {
	// Two clips holding the same notes in different order must print identically,
	// otherwise the rev hash would change without the music changing.
	a := Clip{Name: "x", Bars: 1, SigNum: 4, SigDen: 4, Notes: []Note{
		{Pitch: 60, StartTime: 0, Duration: 1, Velocity: 100},
		{Pitch: 64, StartTime: 0, Duration: 1, Velocity: 100},
	}}
	b := Clip{Name: "x", Bars: 1, SigNum: 4, SigDen: 4, Notes: []Note{
		{Pitch: 64, StartTime: 0, Duration: 1, Velocity: 100},
		{Pitch: 60, StartTime: 0, Duration: 1, Velocity: 100},
	}}
	sa, err := Format(a)
	if err != nil {
		t.Fatalf("Format(a) error = %v", err)
	}
	sb, err := Format(b)
	if err != nil {
		t.Fatalf("Format(b) error = %v", err)
	}
	if sa != sb {
		t.Errorf("same notes printed differently:\n%q\n%q", sa, sb)
	}
}

func TestFormatEmptyClip(t *testing.T) {
	got, err := Format(Clip{Name: "Empty", Bars: 2, SigNum: 4, SigDen: 4})
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	want := "clip \"Empty\" bars=2 sig=4/4\n\n"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestFormatRejectsBadValues(t *testing.T) {
	bad := []Clip{
		{Name: "a", Bars: 1, SigNum: 0, SigDen: 4},
		{Name: "a", Bars: 1, SigNum: 4, SigDen: 4, Notes: []Note{{Pitch: 200, Duration: 1, Velocity: 100}}},
		{Name: "a", Bars: 1, SigNum: 4, SigDen: 4, Notes: []Note{{Pitch: 60, Duration: 1, Velocity: 0}}},
	}
	for i, c := range bad {
		if _, err := Format(c); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
}
