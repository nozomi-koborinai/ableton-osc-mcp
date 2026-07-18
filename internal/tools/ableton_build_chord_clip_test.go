package tools

import (
	"testing"
)

func TestParseChordSymbol(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in        string
		wantName  string
		wantRoot  int
		wantCount int
		wantRest  bool
		wantErr   bool
	}{
		{in: "C", wantName: "C", wantRoot: 0, wantCount: 3},
		{in: "Am", wantName: "Am", wantRoot: 9, wantCount: 3},
		{in: "F#m7", wantName: "F#m7", wantRoot: 6, wantCount: 4},
		{in: "Bb", wantName: "Bb", wantRoot: 10, wantCount: 3},
		{in: "Gsus4", wantName: "Gsus4", wantRoot: 7, wantCount: 3},
		{in: "C5", wantName: "C5", wantRoot: 0, wantCount: 2},
		{in: "Gmaj7", wantName: "Gmaj7", wantRoot: 7, wantCount: 4},
		{in: "N.C.", wantRest: true},
		{in: "-", wantRest: true},
		{in: "Dm", wantName: "Dm", wantRoot: 2, wantCount: 3},
		{in: "H", wantErr: true},
		{in: "Cxyz", wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseChordSymbol(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseChordSymbol(%q) expected error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChordSymbol(%q) error = %v", tc.in, err)
			}
			if tc.wantRest {
				if !got.rest {
					t.Fatalf("expected rest for %q", tc.in)
				}
				return
			}
			if got.rootPC != tc.wantRoot {
				t.Errorf("root = %d, want %d", got.rootPC, tc.wantRoot)
			}
			if len(got.intervals) != tc.wantCount {
				t.Errorf("intervals = %d, want %d", len(got.intervals), tc.wantCount)
			}
		})
	}
}

func TestChordProgressionNotes(t *testing.T) {
	t.Parallel()

	chords, err := parseProgression("C | G | Am | F")
	if err != nil {
		t.Fatalf("parseProgression() error = %v", err)
	}
	notes, names := chordProgressionNotes(chords, 4, defaultChordOctave, 90)
	if len(names) != 4 {
		t.Fatalf("names = %v", names)
	}
	// 4 triads * 3 notes = 12.
	if len(notes) != 12 {
		t.Fatalf("notes = %d, want 12", len(notes))
	}
	// First chord C major at octave 4 -> root MIDI 60, so 60/64/67.
	if notes[0].Pitch != 60 || notes[1].Pitch != 64 || notes[2].Pitch != 67 {
		t.Errorf("C major pitches = %d/%d/%d, want 60/64/67", notes[0].Pitch, notes[1].Pitch, notes[2].Pitch)
	}
	// Second chord starts at beat 4.
	if notes[3].StartTime != 4 {
		t.Errorf("second chord start = %v, want 4", notes[3].StartTime)
	}
}

func TestBuildChordClipStub(t *testing.T) {
	t.Parallel()

	stub := &recipeClientStub{
		queries: map[string][]interface{}{
			"/live/clip_slot/get/has_clip": {int32(1), int32(0), int32(1)},
		},
	}
	tempo := 120.0
	out, err := buildChordClip(stub, BuildChordClipInput{
		TrackIndex:  1,
		ClipIndex:   0,
		Progression: "C G Am F",
		Tempo:       &tempo,
		Fire:        true,
	})
	if err != nil {
		t.Fatalf("buildChordClip() error = %v", err)
	}
	if out.LengthBeats != 16 || out.NotesAdded != 12 {
		t.Errorf("length/notes = %v/%d, want 16/12", out.LengthBeats, out.NotesAdded)
	}
	if out.TempoSet != 120 || !out.Fired {
		t.Errorf("tempo/fired = %v/%v", out.TempoSet, out.Fired)
	}
	assertSent(t, stub, "/live/song/set/tempo")
	assertSent(t, stub, "/live/clip_slot/create_clip")
	assertSent(t, stub, "/live/clip/add/notes")
	assertSent(t, stub, "/live/clip_slot/fire")
}

func TestBuildChordClipRejectsEmpty(t *testing.T) {
	t.Parallel()

	stub := &recipeClientStub{}
	if _, err := buildChordClip(stub, BuildChordClipInput{TrackIndex: 0, ClipIndex: 0, Progression: "   "}); err == nil {
		t.Fatal("expected error for empty progression")
	}
}

func assertSent(t *testing.T, stub *recipeClientStub, address string) {
	t.Helper()
	for _, c := range stub.calls {
		if c.address == address {
			return
		}
	}
	t.Errorf("expected a call to %s", address)
}
