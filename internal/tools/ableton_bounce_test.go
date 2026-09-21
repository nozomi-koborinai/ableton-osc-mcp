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
