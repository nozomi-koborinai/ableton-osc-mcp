package tools

import "testing"

func TestBounceSessionPassReturnsTheRecordedFiles(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.trackNames = []string{"Drums", "Bass", "Bounce"}
	got, err := bounceSessionPass(live, live.deps(), BounceSessionPassInput{SceneIndices: []int{1, 0}, BarsPerScene: 2})
	if err != nil {
		t.Fatalf("bounceSessionPass() error = %v", err)
	}
	if !got.OK || got.TrackName != "Bounce" || got.TrackIndex != 2 || got.RoutingType != "Resampling" {
		t.Errorf("got = %+v", got)
	}
	if len(got.FilePaths) != 1 || got.FilePaths[0] != live.filePath {
		t.Errorf("file_paths = %v, want the recorded file so it can be analyzed", got.FilePaths)
	}
	if len(got.ScenesFired) != 2 || got.BarsPerScene != 2 {
		t.Errorf("scenes/bars = %v/%d, want [1 0]/2", got.ScenesFired, got.BarsPerScene)
	}
	// Two scenes of two bars at 120 BPM. Live records bar to bar, so the file is exactly that long.
	if got.DurationSec != 8 {
		t.Errorf("duration_sec = %v, want 8", got.DurationSec)
	}
	if countCalls(live, "/live/song/stop_all_clips") != 2 || countCalls(live, "/live/song/set/back_to_arranger") != 1 {
		t.Errorf("a bounce starts from a clean slate (Back to Arrangement, all clips stopped) and stops all clips after: %v", live.addresses())
	}
	// Scenes 0 and 1 are launched, so the take sits in row 2 and stays there.
	if !live.hasClip[2][2] {
		t.Error("a bounce keeps its clip (unlike a measurement)")
	}
}

func TestBounceSessionPassKeepsItsDefaults(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.trackNames = []string{"Drums", "Bass", "Bounce"}
	for t := range live.hasClip { // the default pass launches scenes 0-3: give the set five rows
		live.hasClip[t] = []bool{false, false, false, false, false}
		live.stopButton[t] = []bool{true, true, true, true, true}
	}
	got, err := bounceSessionPass(live, live.deps(), BounceSessionPassInput{})
	if err != nil {
		t.Fatalf("bounceSessionPass() error = %v", err)
	}
	// Intro, Verse, Hook, Bridge, Hook at four bars each.
	if len(got.ScenesFired) != 5 || got.ScenesFired[0] != 2 || got.BarsPerScene != 4 {
		t.Errorf("defaults = %v / %d bars, want [2 1 0 3 0] / 4", got.ScenesFired, got.BarsPerScene)
	}
}

func TestBounceSessionPassRecordsASongWithItsTail(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.trackNames = []string{"Drums", "Bass", "Bounce"}
	live.sceneNames = []string{"Hook", "Intro", ""}
	got, err := bounceSessionPass(live, live.deps(), BounceSessionPassInput{
		Sections: []SongSection{{SceneIndex: 1, Bars: 1}, {SceneIndex: 0, Bars: 2, Name: "Hook 1"}},
	})
	if err != nil {
		t.Fatalf("bounceSessionPass() error = %v", err)
	}
	// 1 + 2 bars of song and the default two bars of tail, at 120 BPM in 4/4.
	if got.DurationSec != 10 || got.TailSec != 4 || got.TempoBPM != 120 {
		t.Errorf("duration = %v s, tail = %v s, tempo = %v; want 10, 4, 120", got.DurationSec, got.TailSec, got.TempoBPM)
	}
	want := []BouncedSection{{Name: "Intro", SceneIndex: 1, Bars: 1, StartSec: 0}, {Name: "Hook 1", SceneIndex: 0, Bars: 2, StartSec: 2}}
	if len(got.Sections) != 2 || got.Sections[0] != want[0] || got.Sections[1] != want[1] {
		t.Errorf("sections = %+v, want %+v", got.Sections, want)
	}
	if len(got.ScenesFired) != 2 || got.ScenesFired[0] != 1 || got.ScenesFired[1] != 0 {
		t.Errorf("scenes_fired = %v, want [1 0]", got.ScenesFired)
	}
	if countCalls(live, "/live/track/stop_all_clips") != 2 {
		t.Errorf("the tail should stop Drums and Bass: %v", live.addresses())
	}
}

func TestBounceSessionPassTailIsOptional(t *testing.T) {
	t.Parallel()

	zero := 0
	live := newFakeRecorder()
	live.trackNames = []string{"Drums", "Bass", "Bounce"}
	got, err := bounceSessionPass(live, live.deps(), BounceSessionPassInput{Sections: []SongSection{{SceneIndex: 0, Bars: 2}}, TailBars: &zero})
	if err != nil {
		t.Fatalf("bounceSessionPass() error = %v", err)
	}
	if got.DurationSec != 4 || got.TailSec != 0 || countCalls(live, "/live/track/stop_all_clips") != 0 {
		t.Errorf("duration = %v, tail = %v; want the two bars and nothing more", got.DurationSec, got.TailSec)
	}

	// The old form keeps its old length: no tail unless asked for.
	live = newFakeRecorder()
	live.trackNames = []string{"Drums", "Bass", "Bounce"}
	got, err = bounceSessionPass(live, live.deps(), BounceSessionPassInput{SceneIndices: []int{0}, BarsPerScene: 2})
	if err != nil || got.DurationSec != 4 || got.TailSec != 0 {
		t.Errorf("scene_indices form: duration = %v, tail = %v, err = %v; want 4, 0", got.DurationSec, got.TailSec, err)
	}
}

func TestBounceSessionPassChecksTheSongBeforeTouchingLive(t *testing.T) {
	t.Parallel()

	nine := 9
	for name, input := range map[string]BounceSessionPassInput{
		"both forms":    {Sections: []SongSection{{SceneIndex: 0, Bars: 2}}, SceneIndices: []int{0}},
		"no bars":       {Sections: []SongSection{{SceneIndex: 0, Bars: 0}}},
		"long tail":     {Sections: []SongSection{{SceneIndex: 0, Bars: 2}}, TailBars: &nine},
		"missing scene": {Sections: []SongSection{{SceneIndex: 9, Bars: 2}}},
	} {
		live := newFakeRecorder()
		live.trackNames = []string{"Drums", "Bass", "Bounce"}
		if _, err := bounceSessionPass(live, live.deps(), input); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if len(live.calls) != 0 {
			t.Errorf("%s: Live was touched: %v", name, live.addresses())
		}
	}
}
