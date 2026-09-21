package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/reference"
)

func testMix(lufs, crest, lowDB float64) audioanalyze.MixProfile {
	return audioanalyze.MixProfile{
		LUFSIntegrated: lufs, CrestDB: crest, SamplePeakDBFS: -1,
		Bands: []audioanalyze.BandLevel{{Label: "<60", LoHz: 20, HiHz: 60, DB: lowDB}},
		Width: []audioanalyze.BandWidth{{Label: "1-4k", LoHz: 1000, HiHz: 4000, LRCorrelation: 0.4}},
	}
}

// scriptedMeasureDeps answers each analysis with the next mix in the script and
// remembers the window it was asked for.
type scriptedMeasureDeps struct {
	mixes   []audioanalyze.MixProfile
	windows []audioanalyze.Options
}

func (s *scriptedMeasureDeps) deps(live *fakeRecorder, store referenceStore) measureDeps {
	return measureDeps{
		record:   live.deps(),
		duration: func(string) (float64, error) { return 60, nil },
		analyze: func(_ string, opts audioanalyze.Options) (audioanalyze.Result, error) {
			s.windows = append(s.windows, opts)
			mix := s.mixes[0]
			if len(s.mixes) > 1 {
				s.mixes = s.mixes[1:]
			}
			return audioanalyze.Result{MixProfile: &mix}, nil
		},
		store: store,
	}
}

func countCalls(live *fakeRecorder, address string) int {
	n := 0
	for _, a := range live.addresses() {
		if a == address {
			n++
		}
	}
	return n
}

func TestMeasureMixRecordsMeasuresAndTidiesUp(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-10.5, 9.4, -6)}}
	got, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{SceneIndex: scene(1), Bars: 2})
	if err != nil {
		t.Fatalf("measureMixTool() error = %v", err)
	}
	if got.Mix.LUFSIntegrated != -10.5 || len(got.RecordedFiles) != 1 || got.Reference != nil {
		t.Errorf("got = %+v", got)
	}
	// 120 BPM, record-on at beat 1.5, window 4..12: 1.25 s into the file, 4 s long.
	if w := script.windows[0]; w.StartSec != 1.25 || w.EndSec != 5.25 {
		t.Errorf("analysis window = [%v, %v], want [1.25, 5.25]", w.StartSec, w.EndSec)
	}
	if countCalls(live, "/live/clip_slot/delete_clip") != 1 || live.hasClip[2][0] {
		t.Errorf("the recorded clip should be deleted afterwards; calls = %v", live.addresses())
	}
}

func TestMeasureMixKeepsTheRecordingWhenAsked(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-10, 9, -6)}}
	if _, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{Bars: 1, KeepRecording: true}); err != nil {
		t.Fatalf("measureMixTool() error = %v", err)
	}
	if countCalls(live, "/live/clip_slot/delete_clip") != 0 {
		t.Error("keep_recording must leave the clip in place")
	}
}

func TestMeasureMixDefaultsToEightBars(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-10, 9, -6)}}
	if _, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{}); err != nil {
		t.Fatalf("measureMixTool() error = %v", err)
	}
	// Eight bars of 4/4 at 120 BPM are 16 s.
	if w := script.windows[0]; w.EndSec-w.StartSec != 16 {
		t.Errorf("window length = %v s, want 16", w.EndSec-w.StartSec)
	}
}

func TestMeasureMixComparesWithReferences(t *testing.T) {
	t.Parallel()

	store := newReferenceTestStore(t)
	if _, err := store.Save(reference.Profile{Name: "envy", Mix: testMix(-9.5, 9.6, -13)}); err != nil {
		t.Fatal(err)
	}
	live := newFakeRecorder()
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-10.9, 9.4, -6.5)}}

	got, err := measureMixTool(live, script.deps(live, store), MeasureMixInput{Bars: 2, References: []reference.Weight{{Name: "envy"}}})
	if err != nil {
		t.Fatalf("measureMixTool() error = %v", err)
	}
	if got.Reference == nil || len(got.Reference.OutOfRange) != 1 || got.Reference.OutOfRange[0] != "<60" {
		t.Fatalf("reference = %+v, want <60 flagged (+6.5 dB over the reference)", got.Reference)
	}
	if got.Reference.BandDeltasDB[0].DeltaDB != 6.5 {
		t.Errorf("delta = %v, want 6.5", got.Reference.BandDeltasDB[0].DeltaDB)
	}
}

func TestMeasureMixFailsOnAnUnknownReferenceBeforeRecording(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-10, 9, -6)}}
	_, err := measureMixTool(live, script.deps(live, newReferenceTestStore(t)), MeasureMixInput{References: []reference.Weight{{Name: "nope"}}})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "unknown_reference" {
		t.Fatalf("error = %v, want unknown_reference", err)
	}
	if len(live.calls) != 0 {
		t.Errorf("Live was touched before the references were checked: %v", live.addresses())
	}
}

func TestMeasureMixMeasuresGroupsBySoloingThem(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.solo = []bool{false, true, false} // the listener had Bass soloed
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{
		testMix(-10, 9, -6),     // full mix
		testMix(-17.5, 12, -30), // drums
		testMix(-11, 8.5, -3),   // bass
	}}
	got, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{Bars: 1, Groups: []MeasureGroup{
		{Name: "drums", TrackIndices: []int{0}},
		{Name: "bass", TrackIndices: []int{1}},
	}})
	if err != nil {
		t.Fatalf("measureMixTool() error = %v", err)
	}
	if len(got.Groups) != 2 || got.Groups[0].LevelVsMixDB != -7.5 || got.Groups[1].LevelVsMixDB != -1 {
		t.Errorf("groups = %+v, want drums -7.5 dB and bass -1 dB against the mix", got.Groups)
	}
	if len(got.RecordedFiles) != 3 || countCalls(live, "/live/clip_slot/delete_clip") != 3 {
		t.Errorf("three passes should record and tidy three clips: files = %v", got.RecordedFiles)
	}
	if live.solo[0] || !live.solo[1] || live.solo[2] {
		t.Errorf("solo states = %v, want the listener's own solo (Bass) restored", live.solo)
	}
}

func TestMeasureMixRejectsASilentRecordingAndStillTidiesUp(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	silent := testMix(-120, 0, -120)
	silent.SamplePeakDBFS = -120
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{silent}}
	_, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{Bars: 1})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "recording_silent" {
		t.Fatalf("error = %v, want recording_silent", err)
	}
	if countCalls(live, "/live/clip_slot/delete_clip") != 1 {
		t.Error("a failed measurement should not leave its clip behind")
	}
}

func TestMeasureMixWarnsWhenTheMasterIsSquashed(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-7, 6.5, -6)}}
	got, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{Bars: 1})
	if err != nil {
		t.Fatalf("measureMixTool() error = %v", err)
	}
	if !strings.Contains(got.Note, "master") {
		t.Errorf("note = %q, want a warning that fader moves will not reach the output", got.Note)
	}
}

func TestMeasureMixRefusesAWindowTooShortToAnalyzeBeforeRecording(t *testing.T) {
	t.Parallel()

	// At 300 BPM a 4/4 bar lasts 0.8 s, and the analysis needs a full second.
	// Finding that out after a real-time pass would waste the pass.
	live := newFakeRecorder()
	live.tempo = 300
	script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-10, 9, -6)}}
	_, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{Bars: 1})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "window_too_short" {
		t.Fatalf("error = %v, want window_too_short", err)
	}
	if !strings.Contains(actionableErr.NextStep, "2 bars") {
		t.Errorf("next step = %q, want it to say how many bars are enough (2)", actionableErr.NextStep)
	}
	if len(live.calls) != 0 || len(script.windows) != 0 {
		t.Errorf("Live was touched (%v) or audio analyzed (%d) for a pass that could never be measured", live.addresses(), len(script.windows))
	}

	// Two bars are 1.6 s: fine.
	if _, err := measureMixTool(live, script.deps(live, nil), MeasureMixInput{Bars: 2}); err != nil {
		t.Errorf("two bars at 300 BPM: %v", err)
	}
}

func TestMeasureMixValidatesItsInput(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]MeasureMixInput{
		"too many bars":    {Bars: 65},
		"negative bars":    {Bars: -1},
		"negative scene":   {SceneIndex: scene(-1)},
		"group w/o name":   {Groups: []MeasureGroup{{TrackIndices: []int{0}}}},
		"group w/o tracks": {Groups: []MeasureGroup{{Name: "drums"}}},
		"negative track":   {Groups: []MeasureGroup{{Name: "drums", TrackIndices: []int{-1}}}},
		"too many groups":  {Groups: make([]MeasureGroup, 7)},
	} {
		live := newFakeRecorder()
		script := &scriptedMeasureDeps{mixes: []audioanalyze.MixProfile{testMix(-10, 9, -6)}}
		if _, err := measureMixTool(live, script.deps(live, nil), input); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if len(live.calls) != 0 {
			t.Errorf("%s: Live was touched by an invalid request", name)
		}
	}
}
