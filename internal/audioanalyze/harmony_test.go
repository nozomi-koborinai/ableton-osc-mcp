package audioanalyze

import (
	"reflect"
	"strings"
	"testing"
)

// harmonyBars renders one bar per chord at 120 BPM: a bass note under the chord
// tones, the way an 808 sits under a pad. A bass of 0 leaves the bass out.
type testChord struct {
	bass       int
	notes      []int
	faint      []int // tones that are there at faintLevel of the others
	faintLevel float64
}

func renderChords(chords []testChord, sampleRate int) []float64 {
	var out []float64
	for _, chord := range chords {
		bar := detunedNotes(chord.notes, 0, sampleRate, 2)
		if chord.bass > 0 {
			for i, v := range detunedNotes([]int{chord.bass}, 0, sampleRate, 2) {
				bar[i] += 3 * v
			}
		}
		for i, v := range detunedNotes(chord.faint, 0, sampleRate, 2) {
			bar[i] += chord.faintLevel * v
		}
		out = append(out, bar...)
	}
	return out
}

func harmonyOf(t *testing.T, chords []testChord, key KeyResult) Harmony {
	t.Helper()
	samples := renderChords(chords, 44100)
	grid, ok := buildBeatGrid(samples, 44100, beatGridOptions{BPM: 120, ExactTempo: true, DownbeatSet: true})
	if !ok {
		t.Fatal("no grid")
	}
	got, ok := estimateHarmony(samples, 44100, grid, 0, key, key.Tonic != "")
	if !ok {
		t.Fatal("estimateHarmony returned ok=false")
	}
	return got
}

func chordNames(h Harmony) []string {
	var names []string
	for _, c := range h.Chords {
		names = append(names, c.Chord)
	}
	return names
}

func TestHarmonyNamesExtendedChordsWhenTheExtensionIsReallyThere(t *testing.T) {
	t.Parallel()

	got := harmonyOf(t, []testChord{
		{bass: 36, notes: []int{60, 64, 67, 74}},                                // C E G D   over C
		{bass: 41, notes: []int{65, 69, 72, 76}},                                // F A C E   over F
		{bass: 33, notes: []int{57, 60, 64, 67}},                                // A C E G   over A
		{bass: 38, notes: []int{62, 64, 69}},                                    // D E A     over D
		{bass: 33, notes: []int{57, 62, 64}},                                    // the same three notes over A: the bass names the chord
		{bass: 36, notes: []int{60, 64, 67}, faint: []int{74}, faintLevel: 0.3}, // a ninth that only passes by
		{bass: 36, notes: []int{60, 64, 67}, faint: []int{74}, faintLevel: 0.7}, // a ninth that is part of the chord
	}, KeyResult{})
	want := []string{"Cadd9", "Fmaj7", "Am7", "Dsus2", "Asus4", "C", "Cadd9"}
	if !reflect.DeepEqual(chordNames(got), want) {
		t.Errorf("chords = %v, want %v", chordNames(got), want)
	}
	if first := got.Chords[0]; first.Bar != 1 || first.Beat != 1 || first.Beats != 4 || first.Root != "C" || first.Quality != "add9" || first.Bass != "C" || first.Confidence < 0.5 {
		t.Errorf("first chord = %+v", first)
	}
}

func TestHarmonyWritesTheBassWhenItIsNotTheRoot(t *testing.T) {
	t.Parallel()

	got := harmonyOf(t, []testChord{
		{bass: 40, notes: []int{60, 64, 67, 72}}, // C major over E
		{bass: 36, notes: []int{60, 64, 67, 72}},
	}, KeyResult{})
	if want := []string{"C/E", "C"}; !reflect.DeepEqual(chordNames(got), want) {
		t.Errorf("chords = %v, want %v", chordNames(got), want)
	}
}

func TestHarmonyFindsTheLoopAndItsDegrees(t *testing.T) {
	t.Parallel()

	loop := []testChord{
		{bass: 33, notes: []int{57, 60, 64}}, // Am
		{bass: 41, notes: []int{60, 65, 69}}, // F
		{bass: 36, notes: []int{60, 64, 67}}, // C
		{bass: 43, notes: []int{62, 67, 71}}, // G
	}
	var song []testChord
	for i := 0; i < 4; i++ {
		song = append(song, loop...)
	}
	got := harmonyOf(t, song, KeyResult{Tonic: "A", Scale: "minor", Confidence: 0.5})
	if got.CycleBars != 4 {
		t.Fatalf("cycle_bars = %d, want 4 (chords %v)", got.CycleBars, chordNames(got))
	}
	if want := "Am | F | C | G"; got.Summary != want {
		t.Errorf("summary = %q, want %q", got.Summary, want)
	}
	if want := []string{"i", "i", "♭VI", "♭VI", "♭III", "♭III", "♭VII", "♭VII"}; !reflect.DeepEqual(got.CycleDegrees, want) {
		t.Errorf("cycle_degrees = %v, want %v", got.CycleDegrees, want)
	}
	if got.Chords[1].Degree != "♭VI" || got.Chords[1].Bar != 2 {
		t.Errorf("second chord = %+v, want ♭VI on bar 2", got.Chords[1])
	}
}

func TestHarmonyLongerLoopIsNotMistakenForItsFirstHalf(t *testing.T) {
	t.Parallel()

	// i i VI VII | i III VI VII: the two halves differ in one bar only.
	am, f, g, c := testChord{bass: 33, notes: []int{57, 60, 64}}, testChord{bass: 41, notes: []int{60, 65, 69}}, testChord{bass: 43, notes: []int{62, 67, 71}}, testChord{bass: 36, notes: []int{60, 64, 67}}
	loop := []testChord{am, am, f, g, am, c, f, g}
	got := harmonyOf(t, append(append([]testChord{}, loop...), loop...), KeyResult{Tonic: "A", Scale: "minor", Confidence: 0.5})
	if got.CycleBars != 8 || !strings.HasPrefix(got.Summary, "Am | Am | F | G | Am | C") {
		t.Errorf("cycle_bars = %d, summary = %q; want the eight-bar loop", got.CycleBars, got.Summary)
	}
}

func TestHarmonyLeavesSilenceUnnamed(t *testing.T) {
	t.Parallel()

	got := harmonyOf(t, []testChord{{bass: 36, notes: []int{60, 64, 67}}, {}, {bass: 36, notes: []int{60, 64, 67}}}, KeyResult{})
	if want := []string{"C", "N.C.", "C"}; !reflect.DeepEqual(chordNames(got), want) {
		t.Errorf("chords = %v, want %v", chordNames(got), want)
	}
	if got.Key != "C major" || got.Chords[0].Degree != "I" {
		t.Errorf("key = %q, first degree = %q; want the one chord there is to be home", got.Key, got.Chords[0].Degree)
	}
}

// Seen on a real mix: the same bar came out as C#sus2 in one round and C#add9
// in the next, and a loop compared by full names never repeated. Roots repeat.
func TestHarmonyFindsTheLoopWhenTheColoursOfAChordVary(t *testing.T) {
	t.Parallel()

	am, f := testChord{bass: 33, notes: []int{57, 60, 64}}, testChord{bass: 41, notes: []int{60, 65, 69}}
	amAdd9, fMaj7 := testChord{bass: 33, notes: []int{57, 60, 64, 71}}, testChord{bass: 41, notes: []int{60, 65, 69, 76}}
	c, g, gSus := testChord{bass: 36, notes: []int{60, 64, 67}}, testChord{bass: 43, notes: []int{62, 67, 71}}, testChord{bass: 43, notes: []int{62, 67, 72}}
	song := []testChord{am, f, c, g, amAdd9, f, c, gSus, am, fMaj7, c, g, am, f, c, g}
	got := harmonyOf(t, song, KeyResult{})
	if got.CycleBars != 4 || got.Summary != "Am | F | C | G" {
		t.Errorf("cycle_bars = %d, summary = %q (chords %v); want the four-bar loop in its most usual colours", got.CycleBars, got.Summary, chordNames(got))
	}
}

// Seen on a real mix: the key estimate of the first minute said F major for a
// loop in D# minor, and every degree was measured from the wrong note. The
// loop knows its own home: the root it keeps coming back to.
func TestHarmonyMeasuresDegreesFromTheLoopsOwnHome(t *testing.T) {
	t.Parallel()

	am, f, g, c := testChord{bass: 33, notes: []int{57, 60, 64}}, testChord{bass: 41, notes: []int{60, 65, 69}}, testChord{bass: 43, notes: []int{62, 67, 71}}, testChord{bass: 36, notes: []int{60, 64, 67}}
	loop := []testChord{am, am, f, g, am, c, f, g}
	got := harmonyOf(t, append(append([]testChord{}, loop...), loop...), KeyResult{Tonic: "F", Scale: "major", Confidence: 0.16})
	if got.Key != "A minor" {
		t.Errorf("harmony key = %q, want A minor: the root the loop keeps coming back to, and minor there", got.Key)
	}
	if want := []string{"i", "i", "i", "i", "♭VI", "♭VI", "♭VII", "♭VII", "i", "i", "♭III", "♭III", "♭VI", "♭VI", "♭VII", "♭VII"}; !reflect.DeepEqual(got.CycleDegrees, want) {
		t.Errorf("cycle_degrees = %v, want %v", got.CycleDegrees, want)
	}
	if !strings.Contains(got.Note, "F major") {
		t.Errorf("note = %q; it should say that the overall key estimate (F major) disagrees", got.Note)
	}
}
