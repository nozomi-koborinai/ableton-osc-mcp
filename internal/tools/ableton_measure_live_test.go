package tools

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/reference"
)

// Acceptance tests for the dB mixer and the measure tool against a running
// Ableton Live. The stubs elsewhere in this package prove the logic; these
// prove the claims about Live itself: that the display string is a usable
// ground truth for dB, that a Resampling pass lands in one file across scene
// changes, that the recorded clip can be found and its file read, and that
// the measured window lines up with the bar.
//
// Skipped unless ABLETON_LIVE_TEST is set. Run them in a disposable Live set
// (File > New Live Set): they add a "Probe" MIDI track with Operator, and the
// "Measure" and "Bounce" tracks, record a few bars of audio, and delete their
// tracks again at the end.
//
//	ABLETON_LIVE_TEST=1 go test ./internal/tools/ -run 'TestLiveMixer|TestLiveRecord|TestLiveMeasure' -v -count=1
//
// As with the clip notation test, no other ableton-osc-mcp may hold UDP 11001.
func liveMeasureClient(t *testing.T) *abletonosc.Client {
	t.Helper()
	if os.Getenv("ABLETON_LIVE_TEST") == "" {
		t.Skip("set ABLETON_LIVE_TEST=1 with Ableton Live running (use a disposable set)")
	}
	client, err := abletonosc.NewClient("127.0.0.1", 11000, 11001, 2*time.Second)
	if err != nil {
		t.Fatalf("connect to AbletonOSC: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Query("/live/test"); err != nil {
		t.Fatalf("AbletonOSC does not answer: %v", err)
	}
	return client
}

// liveProbeTrack adds a MIDI track with Operator and a one-bar clip whose only
// note is a short blip on the downbeat, in scenes 0 and 1. The blip makes bar
// starts visible in a recording.
func liveProbeTrack(t *testing.T, client *abletonosc.Client) int {
	t.Helper()
	if err := client.Send("/live/song/create_midi_track", int32(-1)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	n, err := queryNumTracks(client)
	if err != nil {
		t.Fatal(err)
	}
	track := n - 1
	t.Cleanup(func() {
		_ = client.Send("/live/song/stop_playing")
		_ = client.Send("/live/song/stop_all_clips")
		deleteLiveTracksNamed(t, client, "Probe", measureTrackName, "Bounce")
	})
	if err := client.Send("/live/track/set/name", int32(track), "Probe"); err != nil {
		t.Fatal(err)
	}
	res, err := client.QueryWithTimeout(10*time.Second, "/live/track/load/browser_item", int32(track), "Operator")
	if err != nil {
		t.Fatalf("load Operator: %v", err)
	}
	t.Logf("load Operator -> %v", res)
	time.Sleep(time.Second)

	written, err := writeClipNotation(client, ClipWriteInput{
		TrackIndex: track, ClipIndex: 0,
		Notation: "clip \"Blip\" bars=1 sig=4/4\n\n  1:1 C3 1/16 v127\n",
	})
	if err != nil || !written.Verified {
		t.Fatalf("write probe clip: %v (%+v)", err, written)
	}
	if err := client.Send("/live/clip_slot/duplicate_clip_to", int32(track), int32(0), int32(track), int32(1)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	return track
}

func deleteLiveTracksNamed(t *testing.T, client *abletonosc.Client, names ...string) {
	t.Helper()
	res, err := client.Query("/live/song/get/track_names")
	if err != nil {
		t.Logf("WARN could not list tracks for cleanup: %v", err)
		return
	}
	current := toStringSlice(res)
	for i := len(current) - 1; i >= 0; i-- { // from the end, so indices stay valid
		for _, name := range names {
			if current[i] == name {
				if err := client.Send("/live/song/delete_track", int32(i)); err != nil {
					t.Logf("WARN could not delete track %d (%s): %v", i, name, err)
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
}

func realRecordDeps() recordDeps {
	return recordDeps{
		sleep: time.Sleep,
		fileSize: func(path string) (int64, error) {
			info, err := os.Stat(path)
			if err != nil {
				return 0, err
			}
			return info.Size(), nil
		},
	}
}

func TestLiveMixerDB(t *testing.T) {
	client := liveMeasureClient(t)
	track := liveProbeTrack(t, client)

	for _, want := range []float64{-6, -24, -40, 0, 6} {
		got, err := setTrackVolume(client, SetTrackVolumeInput{TrackIndex: track, DB: &want})
		if err != nil {
			t.Fatalf("set %v dB: %v", want, err)
		}
		shown, silent, err := parseDBDisplay(got.Display)
		t.Logf("asked %6.1f dB -> Live shows %q (raw %.4f)", want, got.Display, got.Value)
		if err != nil || silent || math.Abs(shown-want) > 0.05 {
			t.Errorf("asked %v dB, Live shows %q", want, got.Display)
		}
	}

	minus24 := -24.0
	if _, err := setTrackVolume(client, SetTrackVolumeInput{TrackIndex: track, DB: &minus24}); err != nil {
		t.Fatal(err)
	}
	delta := -2.0
	got, err := setTrackVolume(client, SetTrackVolumeInput{TrackIndex: track, DeltaDB: &delta})
	if err != nil {
		t.Fatalf("delta: %v", err)
	}
	t.Logf("-24 dB then delta -2 -> %q", got.Display)
	if shown, _, _ := parseDBDisplay(got.Display); math.Abs(shown-(-26)) > 0.11 {
		t.Errorf("delta_db -2 from -24 dB shows %q, want -26.0 dB", got.Display)
	}

	tooHigh := 10.0
	before, _ := queryMixerLevel(client, trackVolumeTarget(track))
	_, err = setTrackVolume(client, SetTrackVolumeInput{TrackIndex: track, DB: &tooHigh})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "level_out_of_range" {
		t.Errorf("+10 dB on a track fader: error = %v, want level_out_of_range", err)
	} else {
		t.Logf("+10 dB -> %s", actionableErr.Message)
	}
	after, _ := queryMixerLevel(client, trackVolumeTarget(track))
	if before.Raw != after.Raw {
		t.Errorf("a refused level still moved the fader: %v -> %v", before.Raw, after.Raw)
	}

	// Sends and master, if the set has a return track.
	minus12 := -12.0
	send, err := setTrackSend(client, SetTrackSendInput{TrackIndex: track, SendIndex: 0, DB: &minus12})
	if err != nil {
		t.Logf("send check skipped or failed: %v", err)
	} else {
		t.Logf("send A -12 dB -> %q", send.Display)
		if shown, _, _ := parseDBDisplay(send.Display); math.Abs(shown-(-12)) > 0.05 {
			t.Errorf("send shows %q, want -12.0 dB", send.Display)
		}
		off := -70.0
		if _, err := setTrackSend(client, SetTrackSendInput{TrackIndex: track, SendIndex: 0, DB: &off}); err != nil {
			t.Errorf("turn the send off again: %v", err)
		}
	}
	master, err := queryMixerLevel(client, masterVolumeTarget())
	if err != nil {
		t.Fatalf("read master: %v", err)
	}
	minus3 := -3.0
	moved, err := setMasterVolume(client, SetMasterVolumeInput{DeltaDB: &minus3})
	if err != nil {
		t.Fatalf("master delta: %v", err)
	}
	t.Logf("master %q, delta -3 -> %q", master.Display, moved.Display)
	if _, err := setMasterVolume(client, SetMasterVolumeInput{Volume: &master.Raw}); err != nil {
		t.Errorf("restore master volume: %v", err)
	}

	snapshot, err := captureMixSnapshot(client, []int{track})
	if err != nil || len(snapshot.Tracks) != 1 || snapshot.Tracks[0].VolumeDB == "" {
		t.Errorf("snapshot = %+v, %v; want volume_db filled in", snapshot, err)
	}
}

func TestLiveRecordPassStaysInOneFileAcrossScenes(t *testing.T) {
	client := liveMeasureClient(t)
	liveProbeTrack(t, client)

	got, err := bounceSessionPass(client, realRecordDeps(), BounceSessionPassInput{SceneIndices: []int{0, 1}, BarsPerScene: 2})
	if err != nil {
		t.Fatalf("bounceSessionPass() error = %v", err)
	}
	t.Logf("bounce: %+v", got)
	if len(got.FilePaths) != 1 {
		t.Fatalf("file_paths = %v, want one file across two scene launches", got.FilePaths)
	}
	total, err := audioanalyze.ProbeDuration(got.FilePaths[0])
	if err != nil {
		t.Fatalf("read the recorded file: %v", err)
	}
	t.Logf("recorded %.3f s into %s (engine says %.3f s between record on and off)", total, filepath.Base(got.FilePaths[0]), got.DurationSec)
}

func TestLiveRecordWindowLinesUpWithTheBar(t *testing.T) {
	client := liveMeasureClient(t)
	liveProbeTrack(t, client)

	scene0 := 0
	take, err := recordResampledPass(client, realRecordDeps(), recordPlan{
		TrackName: measureTrackName,
		Spans:     []recordSpan{{SceneIndex: &scene0, Bars: 2}},
	})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	t.Logf("take: %+v", take)
	if len(take.FilePaths) != 1 {
		t.Fatalf("file paths = %v, want one", take.FilePaths)
	}
	total, err := audioanalyze.ProbeDuration(take.FilePaths[0])
	if err != nil {
		t.Fatal(err)
	}
	secPerBeat := 60 / take.TempoBPM
	t.Logf("file %.3f s; if recording starts at once: %.3f s; if it waits for the bar: %.3f s",
		total, (take.RecordOffBeat-take.RecordOnBeat)*secPerBeat, (take.RecordOffBeat-take.WindowStart)*secPerBeat)

	start, end, err := locateWindow(total, take)
	if err != nil {
		t.Fatalf("locateWindow: %v", err)
	}
	res, err := audioanalyze.AnalyzeFile(take.FilePaths[0], audioanalyze.Options{StartSec: start, EndSec: end})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("window [%.3f, %.3f] s, %d onsets", start, end, len(res.Onsets))
	for _, o := range res.Onsets {
		t.Logf("  onset at %.3f s (strength %.2f)", o.Sec, o.Strength)
	}
	// The blip sits on every downbeat: the first one must open the window.
	if len(res.Onsets) == 0 || res.Onsets[0].Sec > 0.05 {
		t.Errorf("first onset = %+v, want the downbeat blip within 50 ms of the window start", res.Onsets)
	}
	barSec := float64(take.BeatsPerBar) * secPerBeat
	if len(res.Onsets) >= 2 && math.Abs((res.Onsets[1].Sec-res.Onsets[0].Sec)-barSec) > 0.03 {
		t.Errorf("blips are %.3f s apart, want one bar (%.3f s)", res.Onsets[1].Sec-res.Onsets[0].Sec, barSec)
	}
}

func TestLiveMeasureMix(t *testing.T) {
	client := liveMeasureClient(t)
	track := liveProbeTrack(t, client)

	store, err := reference.NewStore(filepath.Join(t.TempDir(), "refs.json"))
	if err != nil {
		t.Fatal(err)
	}
	deps := measureDeps{record: realRecordDeps(), analyze: audioanalyze.AnalyzeFile, duration: audioanalyze.ProbeDuration, store: store}

	scene0 := 0
	first, err := measureMixTool(client, deps, MeasureMixInput{SceneIndex: &scene0, Bars: 2})
	if err != nil {
		t.Fatalf("measureMixTool() error = %v", err)
	}
	t.Logf("mix: LUFS %.2f, TP %.2f, crest %.2f, analyzed %.2f s, took %.1f s", first.Mix.LUFSIntegrated, first.Mix.TruePeakDBTP, first.Mix.CrestDB, first.Mix.AnalyzedSec, first.DurationSec)
	t.Logf("files: %v", first.RecordedFiles)
	tempo, _ := queryAuditionTempo(client)
	if want := 2 * 4 * 60 / tempo; math.Abs(first.Mix.AnalyzedSec-want) > 0.06 {
		t.Errorf("analyzed %.3f s, want two bars = %.3f s", first.Mix.AnalyzedSec, want)
	}

	if _, err := store.Save(reference.Profile{Name: "self", Mix: first.Mix}); err != nil {
		t.Fatal(err)
	}
	// Second pass without a scene: measure what is already playing, against the first.
	second, err := measureMixTool(client, deps, MeasureMixInput{Bars: 2, References: []reference.Weight{{Name: "self"}},
		Groups: []MeasureGroup{{Name: "probe", TrackIndices: []int{track}}}})
	if err != nil {
		t.Fatalf("second measureMixTool() error = %v", err)
	}
	t.Logf("against itself: LUFS delta %.2f, out of range %v", second.Reference.LUFSDelta, second.Reference.OutOfRange)
	if math.Abs(second.Reference.LUFSDelta) > 1 {
		t.Errorf("the same material measured twice differs by %.2f LU", second.Reference.LUFSDelta)
	}
	if len(second.Groups) != 1 || math.Abs(second.Groups[0].LevelVsMixDB) > 1 {
		t.Errorf("the only sounding track soloed should match the mix: %+v", second.Groups)
	}

	// The tool tidies up after itself.
	names, _ := client.Query("/live/song/get/track_names")
	for i, name := range toStringSlice(names) {
		if name != measureTrackName {
			continue
		}
		slots, err := occupiedSlots(client, i)
		if err != nil {
			t.Fatal(err)
		}
		for slot, has := range slots {
			if has {
				t.Errorf("Measure track still holds a clip in slot %d", slot)
			}
		}
	}
	if !strings.Contains(first.Note, "recordings folder") {
		t.Errorf("note = %q", first.Note)
	}
}
