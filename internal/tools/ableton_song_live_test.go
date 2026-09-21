package tools

import (
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

// Acceptance tests for "song and delivery" against a running Ableton Live:
// a bounce of sections with a ring-out tail, finalized into a delivery file,
// and the Arrangement written from the same sections. Skipped unless
// ABLETON_LIVE_TEST is set; run them in a disposable Live set.
//
//	ABLETON_LIVE_TEST=1 go test ./internal/tools/ -run 'TestLiveBounceSong|TestLiveArrangement' -v -count=1

// The probe clip is a blip on every downbeat. Two bars of scene 0, one bar of
// scene 1, then one bar in which everything is stopped: the file has to be four
// bars long, with three blips and none in the last bar, and it has to end in
// silence, which is what makes it a file that can be delivered.
func TestLiveBounceSongWithTail(t *testing.T) {
	client := liveMeasureClient(t)
	liveProbeTrack(t, client)

	one := 1
	got, err := bounceSessionPass(client, realRecordDeps(), BounceSessionPassInput{
		Sections: []SongSection{{SceneIndex: 0, Bars: 2, Name: "Verse"}, {SceneIndex: 1, Bars: 1}},
		TailBars: &one,
	})
	if err != nil {
		t.Fatalf("bounceSessionPass() error = %v", err)
	}
	t.Logf("bounce: %+v", got)
	barSec := 4 * 60 / got.TempoBPM
	if len(got.FilePaths) != 1 || math.Abs(got.DurationSec-4*barSec) > 1e-6 || math.Abs(got.TailSec-barSec) > 0.01 {
		t.Fatalf("files = %v, duration = %v, tail = %v; want one file of four bars with a one-bar tail", got.FilePaths, got.DurationSec, got.TailSec)
	}
	if len(got.Sections) != 2 || got.Sections[0].Name != "Verse" || got.Sections[1].StartSec != round2(2*barSec) {
		t.Errorf("sections = %+v", got.Sections)
	}

	total, err := audioanalyze.ProbeDuration(got.FilePaths[0])
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(total-4*barSec) > 0.05 {
		t.Errorf("the file is %.3f s long, want %.3f: the take must run through the tail", total, 4*barSec)
	}
	analysis, err := audioanalyze.AnalyzeFile(got.FilePaths[0], audioanalyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	blips := map[int]bool{}
	for _, onset := range analysis.Onsets {
		t.Logf("  onset at %.3f s (strength %.2f)", onset.Sec, onset.Strength)
		blips[int(math.Round(onset.Sec/barSec))] = true
		if onset.Sec > 3*barSec-0.1 {
			t.Errorf("an onset at %.3f s: the clips should have stopped on the bar line at %.1f s", onset.Sec, 3*barSec)
		}
	}
	if !blips[0] || !blips[1] || !blips[2] {
		t.Errorf("blips found in bars %v, want one on each of the first three downbeats", blips)
	}

	opts := audioanalyze.DefaultFinalizeOptions()
	opts.OutputPath = filepath.Join(t.TempDir(), "delivery.wav")
	delivery, err := audioanalyze.Finalize(got.FilePaths[0], opts)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	t.Logf("delivery: %+v", delivery)
	if !delivery.OK || delivery.SampleRate != 44100 || delivery.BitDepth != 24 {
		t.Errorf("delivery = %+v; want a clean 44.1 kHz / 24-bit file", delivery)
	}
	if delivery.Delivered.TruePeakDBTP > -0.95 {
		t.Errorf("delivered true peak = %.2f dBTP, want -1 or below", delivery.Delivered.TruePeakDBTP)
	}
	// The last blip is on bar 3; after it the file is silence, and that is cut.
	if delivery.TrimmedSec < barSec/2 || delivery.DurationSec > 3*barSec {
		t.Errorf("trimmed %.2f s, %.2f s left; want most of the silent tail gone", delivery.TrimmedSec, delivery.DurationSec)
	}
}

// The probe track has a one-bar blip clip in scenes 0 and 1. Two sections of it
// go into the Arrangement, far out at bar 201 where a disposable set has nothing,
// are read back, refused a second time, rewritten with overwrite, and removed.
func TestLiveArrangementWriteReadAndRewrite(t *testing.T) {
	client := liveMeasureClient(t)
	probe := liveProbeTrack(t, client)
	const startBar = 201
	from, to := float32((startBar-1)*4), float32((startBar-1)*4+64)
	t.Cleanup(func() {
		names, _ := client.Query("/live/song/get/track_names")
		for track := range toStringSlice(names) {
			_, _ = client.Query("/live/track/delete_arrangement_clips", int32(track), from, to)
		}
		deleteLiveTracksNamed(t, client, arrangementSectionsTrack)
	})

	song := []SongSection{{SceneIndex: 0, Bars: 2, Name: "Verse"}, {SceneIndex: 1, Bars: 1, Name: "Hook"}}
	written, err := writeArrangement(client, time.Sleep, WriteArrangementInput{Sections: song, StartBar: startBar})
	if err != nil {
		t.Fatalf("writeArrangement() error = %v", err)
	}
	t.Logf("written: %+v", written)
	if !written.Verified || written.ClipsPlaced != 3 || written.EndBar != startBar+2 || len(written.TracksWritten) != 1 || written.TracksWritten[0] != probe {
		t.Errorf("written = %+v; want three verified clips on the probe track, bars %d-%d", written, startBar, startBar+2)
	}

	read, err := getArrangement(client, GetArrangementInput{FromBar: startBar})
	if err != nil {
		t.Fatalf("getArrangement() error = %v", err)
	}
	t.Logf("read back: %+v", read)
	wantSections := []ArrangementSpan{{Name: "Verse", StartBar: startBar, Bars: 2}, {Name: "Hook", StartBar: startBar + 2, Bars: 1}}
	if len(read.Sections) != 2 || read.Sections[0] != wantSections[0] || read.Sections[1] != wantSections[1] {
		t.Errorf("sections = %+v, want %+v", read.Sections, wantSections)
	}
	if len(read.Tracks) != 1 || read.Tracks[0].TrackIndex != probe || len(read.Tracks[0].Clips) != 1 || read.Tracks[0].Clips[0].Repeats != 3 {
		t.Errorf("tracks = %+v; want the probe track with its blip three times in a row", read.Tracks)
	}

	// A second write finds the first in the way, and says what.
	_, err = writeArrangement(client, time.Sleep, WriteArrangementInput{Sections: song, StartBar: startBar})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "arrangement_occupied" {
		t.Fatalf("second write: error = %v, want arrangement_occupied", err)
	}
	t.Logf("refused: %s", actionableErr.Message)

	// With overwrite the song is replaced: one section now, and nothing left of the longer one.
	rewritten, err := writeArrangement(client, time.Sleep, WriteArrangementInput{Sections: song[:1], StartBar: startBar, Overwrite: true})
	if err != nil {
		t.Fatalf("rewrite: error = %v", err)
	}
	if !rewritten.Verified || rewritten.ClipsReplaced != 5 {
		t.Errorf("rewritten = %+v; want it verified, with the three blips and the two markers of the first version replaced", rewritten)
	}
	read, err = getArrangement(client, GetArrangementInput{FromBar: startBar})
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Sections) != 1 || read.Sections[0].Name != "Verse" || len(read.Tracks) != 1 || read.Tracks[0].Clips[0].Repeats != 2 {
		t.Errorf("after the rewrite: %+v", read)
	}
	if slot := livePlayingSlot(t, client, probe); slot >= 0 {
		t.Errorf("the probe track plays slot %d; writing the Arrangement should start nothing", slot)
	}
}
