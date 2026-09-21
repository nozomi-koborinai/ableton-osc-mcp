package audioanalyze

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeTestBeat(t *testing.T) (string, testBeat) {
	t.Helper()
	beat := defaultTestBeat()
	var buf bytes.Buffer
	samples := beat.render()
	if err := writeWAV(&buf, [][]float64{samples, samples}, beat.sampleRate, 16, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "beat.wav")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, beat
}

func TestAnalyzeGoesDeepOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	path, beat := writeTestBeat(t)
	plain, err := AnalyzeFile(path, Options{})
	if err != nil {
		t.Fatalf("AnalyzeFile() error = %v", err)
	}
	if plain.Tuning == nil {
		t.Error("tuning is measured on every analysis")
	}
	if plain.Grid != nil || plain.Harmony != nil || plain.DrumGrid != nil {
		t.Errorf("grid = %v, harmony = %v, drum grid = %v; a plain analysis (a mix measurement) must not pay for these", plain.Grid, plain.Harmony, plain.DrumGrid)
	}

	deep, err := AnalyzeFile(path, Options{Deep: true, ProjectTempo: beat.bpm, DownbeatSec: beat.offsetSec, DownbeatSet: true})
	if err != nil {
		t.Fatalf("AnalyzeFile(deep) error = %v", err)
	}
	if deep.Grid == nil || deep.Harmony == nil || deep.DrumGrid == nil {
		t.Fatalf("grid = %v, harmony = %v, drum grid = %v; want all three", deep.Grid, deep.Harmony, deep.DrumGrid)
	}
	if deep.Grid.BPM != 140 || deep.Grid.DownbeatConfidence != 1 || deep.Grid.Bars != 8 {
		t.Errorf("grid = %+v, want the project tempo and the given downbeat taken as they are", *deep.Grid)
	}
	if got := deep.DrumGrid.Lanes[0].Pattern; got != "x.....x.....x...|...x......x....." {
		t.Errorf("low lane = %q", got)
	}
	if len(deep.Harmony.Chords) == 0 || deep.Harmony.Chords[0].Root != "C" {
		t.Errorf("harmony = %+v, want it to open on C", deep.Harmony.Chords)
	}
}

func TestAnalyzeDeepFindsItsOwnGrid(t *testing.T) {
	t.Parallel()

	path, beat := writeTestBeat(t)
	deep, err := AnalyzeFile(path, Options{Deep: true})
	if err != nil {
		t.Fatalf("AnalyzeFile(deep) error = %v", err)
	}
	if deep.Grid == nil || deep.DrumGrid == nil {
		t.Fatalf("no grid from an estimated tempo of %.1f BPM: %s", deep.EstimatedBPM, deep.Note)
	}
	// Whatever octave of the tempo the estimate lands on, the hats tick evenly.
	if high := deep.DrumGrid.Lanes[2].Pattern; high != "x.x.x.x.x.x.x.x.|x.x.x.x.x.x.x.x." && high != "xxxxxxxxxxxxxxxx|xxxxxxxxxxxxxxxx" && high != "x...x...x...x...|x...x...x...x..." {
		t.Errorf("high lane = %q at %.2f BPM (true tempo %.0f)", high, deep.Grid.BPM, beat.bpm)
	}
}

func TestAnalyzeRejectsADownbeatOutsideTheAudio(t *testing.T) {
	t.Parallel()

	path, _ := writeTestBeat(t)
	for name, opts := range map[string]Options{
		"negative":         {Deep: true, DownbeatSec: -1, DownbeatSet: true},
		"past the end":     {Deep: true, DownbeatSec: 9999, DownbeatSet: true},
		"without deep set": {DownbeatSec: 0.37, DownbeatSet: true},
	} {
		if _, err := AnalyzeFile(path, opts); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
