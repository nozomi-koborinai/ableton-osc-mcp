package tools

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/notation"
)

// This is the acceptance test for clip notation against a running Ableton Live.
// Everything else in this package stands in for Live with a stub, which proves
// the logic but not the claim that the round trip closes on the real thing.
//
// It is skipped unless ABLETON_LIVE_TEST is set, so CI stays hermetic. Run it as:
//
//	ABLETON_LIVE_TEST=1 go test ./internal/tools/ -run TestLive -v
//
// The target slot must be empty; the clip it creates is removed at the end.
// Override the target with ABLETON_LIVE_TEST_TRACK and ABLETON_LIVE_TEST_SLOT.
func TestLiveClipNotationRoundTrip(t *testing.T) {
	client, track, slot := liveTestTarget(t)

	input := ClipWriteInput{
		TrackIndex: track,
		ClipIndex:  slot,
		Notation: "clip \"Notation Probe\" bars=4 sig=4/4\n\n" +
			"  1:1 C3 1/8 v100\n" +
			"  1:3 D#3 1/8 v92\n" +
			"  2:1 G3 1/4 v104\n" +
			"  2:3 A#3 1/8 v88 -\n" +
			"  3:1.5 C4 1/8. v96\n" +
			"  4:1 D#4 0.333333 v90\n",
		Rev: "",
	}

	created, err := writeClipNotation(client, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		if err := client.Send("/live/clip_slot/delete_clip", int32(track), int32(slot)); err != nil {
			t.Logf("WARN could not remove the probe clip at [%d,%d]: %v", track, slot, err)
		}
	}()
	if !created.Verified {
		t.Fatalf("creating the clip did not verify: %+v", created.Mismatches)
	}
	if created.NotesWritten != 6 {
		t.Errorf("NotesWritten = %d, want 6", created.NotesWritten)
	}

	// Reading it back has to produce the same notation that was written, not just
	// notes that compare equal. If the text differs, the rev would drift and every
	// later write would be refused.
	read, err := readClipNotation(client, ClipReadInput{TrackIndex: track, ClipIndex: slot})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.Notation != input.Notation {
		t.Errorf("notation changed through Live:\nwrote %q\nread  %q", input.Notation, read.Notation)
	}
	if read.Rev != created.Rev {
		t.Errorf("rev drifted between write and read: %q vs %q", created.Rev, read.Rev)
	}

	// An edit on top of the current rev has to go through and verify.
	edited := strings.TrimSuffix(read.Notation, "\n") + "\n  4:3 G4 1/8 v80\n"
	after, err := writeClipNotation(client, ClipWriteInput{
		TrackIndex: track, ClipIndex: slot, Notation: edited, Rev: read.Rev,
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !after.Verified {
		t.Errorf("the edit did not verify: %+v", after.Mismatches)
	}
	if after.NotesWritten != 7 {
		t.Errorf("NotesWritten = %d, want 7", after.NotesWritten)
	}

	// Writing again with the rev from before the edit stands in for someone having
	// moved notes in Live. This is the safety net the whole design rests on.
	_, err = writeClipNotation(client, ClipWriteInput{
		TrackIndex: track, ClipIndex: slot, Notation: edited, Rev: read.Rev,
	})
	assertActionable(t, err, "rev_mismatch")
}

// TestLiveClipNotationToleranceHolds writes positions far into a long clip, where
// float32 rounding is largest, and checks the tolerance still calls them equal.
func TestLiveClipNotationToleranceHolds(t *testing.T) {
	client, track, slot := liveTestTarget(t)

	text := "clip \"Far Probe\" bars=128 sig=4/4\n\n" +
		"  1:1 C3 1/8 v100\n" +
		"  32:2.3333 D3 1/8 v100\n" +
		"  64:1.0625 E3 1/8 v100\n" +
		"  128:3.7519 F3 1.9377 v100\n"

	out, err := writeClipNotation(client, ClipWriteInput{
		TrackIndex: track, ClipIndex: slot, Notation: text, Rev: "",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		if err := client.Send("/live/clip_slot/delete_clip", int32(track), int32(slot)); err != nil {
			t.Logf("WARN could not remove the probe clip at [%d,%d]: %v", track, slot, err)
		}
	}()
	if !out.Verified {
		t.Errorf("far positions did not verify with BeatTolerance=%v: %+v", notation.BeatTolerance, out.Mismatches)
	}
}

// liveTestTarget connects to Live and returns an empty slot to work in.
func liveTestTarget(t *testing.T) (*abletonosc.Client, int, int) {
	t.Helper()
	if os.Getenv("ABLETON_LIVE_TEST") == "" {
		t.Skip("set ABLETON_LIVE_TEST=1 with Ableton Live running to exercise the real round trip")
	}

	track := liveTestEnvInt(t, "ABLETON_LIVE_TEST_TRACK", 0)
	slot := liveTestEnvInt(t, "ABLETON_LIVE_TEST_SLOT", 7)

	client, err := abletonosc.NewClient("127.0.0.1", 11000, 11001, 2*time.Second)
	if err != nil {
		t.Fatalf("connect to AbletonOSC: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	has, err := queryBool(client, "/live/clip_slot/get/has_clip", int32(track), int32(slot))
	if err != nil {
		t.Fatalf("check slot [%d,%d]: %v", track, slot, err)
	}
	if has {
		t.Fatalf("slot [%d,%d] already holds a clip; point the test at an empty one", track, slot)
	}
	return client, track, slot
}

func liveTestEnvInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s must be a whole number, got %q", key, raw)
	}
	return v
}
