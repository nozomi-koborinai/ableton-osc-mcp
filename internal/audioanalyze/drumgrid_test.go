package audioanalyze

import "testing"

func drumGridOf(t *testing.T, beat testBeat) (DrumGrid, bool) {
	t.Helper()
	samples := beat.render()
	grid, ok := buildBeatGrid(samples, beat.sampleRate, beatGridOptions{BPM: beat.bpm, ExactTempo: true, DownbeatSec: beat.offsetSec, DownbeatSet: true})
	if !ok {
		return DrumGrid{}, false
	}
	return estimateDrumGrid(samples, beat.sampleRate, grid)
}

func TestDrumGridReadsThePatternOfEachBand(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"low":  "x.....x.....x...|...x......x.....",
		"mid":  "........x.......|........x.......",
		"high": "x.x.x.x.x.x.x.x.|x.x.x.x.x.x.x.x.",
	}
	for name, pad := range map[string]bool{"chords changing quietly on every bar": false, "a held pad as loud as the drums": true} {
		beat := defaultTestBeat()
		if beat.pad = pad; pad {
			beat.chordLevel = 0.3
		}
		got, ok := drumGridOf(t, beat)
		if !ok {
			t.Fatalf("%s: estimateDrumGrid returned ok=false", name)
		}
		if got.CycleBars != 2 || got.StepsPerBar != 16 || got.BarsAnalyzed != 8 || len(got.Lanes) != 3 || got.Note == "" {
			t.Fatalf("%s: grid = %+v", name, got)
		}
		for _, lane := range got.Lanes {
			if lane.Pattern != want[lane.Name] {
				t.Errorf("%s: %s = %q, want %q", name, lane.Name, lane.Pattern, want[lane.Name])
			}
		}
	}
}

func TestDrumGridSaysHowOftenAStepSounds(t *testing.T) {
	t.Parallel()

	got, ok := drumGridOf(t, defaultTestBeat())
	if !ok {
		t.Fatal("no drum grid")
	}
	low := got.Lanes[0]
	if low.Name != "low" || low.Hears == "" || len(low.Steps) != 5 {
		t.Fatalf("low lane = %+v, want its five steps", low)
	}
	for i, step := range []int{0, 6, 12, 19, 26} {
		if low.Steps[i].Step != step || low.Steps[i].Share < 0.9 || low.Steps[i].Strength <= 0 || low.Steps[i].Strength > 1 {
			t.Errorf("low step %d = %+v, want step %d in every round", i, low.Steps[i], step)
		}
	}
}

func TestDrumGridNeedsAFewRounds(t *testing.T) {
	t.Parallel()

	short := defaultTestBeat()
	short.bars = 3
	if _, ok := drumGridOf(t, short); ok {
		t.Error("three bars are one round and a half: not enough to call anything a pattern")
	}
}
