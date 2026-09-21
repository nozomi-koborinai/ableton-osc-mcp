package tools

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// Acceptance tests for ableton_audition against a running Ableton Live. The
// fake Live elsewhere in this package proves the logic; these prove the claims
// about Live itself: that clips, faders and devices really are what the
// indicator track says while a variant sounds, that everything comes back, and
// what happens when the listener stops playback halfway.
//
// Skipped unless ABLETON_LIVE_TEST is set. Run them in a disposable Live set
// (File > New Live Set); they add a "Probe" track and the "Audition" indicator
// and delete both again at the end.
//
//	ABLETON_LIVE_TEST=1 go test ./internal/tools/ -run 'TestLiveAudition' -v -count=1

// liveAuditionSetup gives the probe track a known level, a device to switch,
// and something playing. It returns the probe track and the device index.
func liveAuditionSetup(t *testing.T, client *abletonosc.Client) (int, int) {
	t.Helper()
	probe := liveProbeTrack(t, client)
	t.Cleanup(func() { deleteLiveTracksNamed(t, client, auditionIndicatorName) })

	// An effect behind Operator is the realistic thing to switch. Where Utility
	// lives depends on the Live version; failing both, Operator itself will do.
	device := 0
	for _, path := range [][]interface{}{
		{int32(probe), int32(-1), "Audio Effects", "Utilities", "Utility"},
		{int32(probe), int32(-1), "Audio Effects", "Utility"},
	} {
		res, err := client.QueryWithTimeout(10*time.Second, "/live/browser/load_at_path", path...)
		if err == nil && len(res) >= 2 && res[1] == "loaded" {
			device = 1
			time.Sleep(500 * time.Millisecond)
			break
		}
	}
	if device == 0 {
		t.Log("could not load Utility; switching Operator instead")
	}

	if _, err := applyLevelChange(client, trackVolumeTarget(probe), levelChange{DB: ptr(-6)}, "volume"); err != nil {
		t.Fatalf("set the probe level: %v", err)
	}
	if err := client.Send("/live/clip_slot/fire", int32(probe), int32(0)); err != nil {
		t.Fatal(err)
	}
	waitForLive(t, "the probe clip to play", func() bool {
		return livePlayingSlot(t, client, probe) == 0
	})
	return probe, device
}

func waitForLive(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func livePlayingSlot(t *testing.T, client *abletonosc.Client, track int) int {
	t.Helper()
	res, err := client.Query("/live/track/get/playing_slot_index", int32(track))
	if err != nil || len(res) < 2 {
		t.Fatalf("playing_slot_index: %v %v", res, err)
	}
	slot, err := abletonosc.AsInt(res[1])
	if err != nil {
		t.Fatal(err)
	}
	return slot
}

func liveIndicatorName(t *testing.T, client *abletonosc.Client) string {
	t.Helper()
	res, err := client.Query("/live/song/get/track_names")
	if err != nil {
		t.Fatalf("track names: %v", err)
	}
	names := toStringSlice(res)
	for i := len(names) - 1; i >= 0; i-- {
		if strings.HasPrefix(names[i], auditionIndicatorName) {
			return names[i]
		}
	}
	return ""
}

// liveAuditionSample is what Live says at one moment. It only counts when the
// indicator read the same before and after the other three questions.
type liveAuditionSample struct {
	name   string
	slot   int
	db     float64
	active bool
}

// sameLevel allows for the third decimal: Live resolves "-6 dB" to the nearest
// fader position, which it then reports as -6.002 dB.
func sameLevel(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func sampleLiveAudition(t *testing.T, client *abletonosc.Client, probe, device int) (liveAuditionSample, bool) {
	t.Helper()
	before := liveIndicatorName(t, client)
	level, err := queryMixerLevel(client, trackVolumeTarget(probe))
	if err != nil {
		t.Fatalf("level: %v", err)
	}
	active, err := queryDeviceOn(client, probe, device)
	if err != nil {
		t.Fatalf("device on: %v", err)
	}
	sample := liveAuditionSample{name: before, slot: livePlayingSlot(t, client, probe), db: level.DB, active: active}
	return sample, liveIndicatorName(t, client) == before
}

func liveAuditionVariants(probe, device int) []AuditionVariant {
	return []AuditionVariant{
		{Label: "X", Description: "as it is"},
		{Label: "B", Description: "the other clip", Clips: []AuditionClip{{TrackIndex: probe, ClipIndex: 1}}},
		{Label: "C", Description: "-6 dB, device off", Mix: []AuditionMix{{TrackIndex: probe, DeltaDB: -6}}, Devices: []AuditionDevice{{TrackIndex: probe, DeviceIndex: device, Active: false}}},
	}
}

func TestLiveAudition(t *testing.T) {
	client := liveMeasureClient(t)
	probe, device := liveAuditionSetup(t, client)
	quantization, err := queryClipTriggerQuantization(client)
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		out AuditionOutput
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := runAudition(client, time.Sleep, AuditionInput{Variants: liveAuditionVariants(probe, device), BarsPerVariant: 2})
		done <- outcome{out, err}
	}()

	var samples []liveAuditionSample
	var result outcome
	for running := true; running; {
		select {
		case result = <-done:
			running = false
		default:
			if sample, steady := sampleLiveAudition(t, client, probe, device); steady {
				samples = append(samples, sample)
			}
		}
	}
	if result.err != nil {
		t.Fatalf("runAudition() error = %v", result.err)
	}
	if len(result.out.Played) != 3 || !result.out.Restored {
		t.Fatalf("output = %+v, want three variants played and restored", result.out)
	}
	t.Logf("played %+v", result.out.Played)

	// What Live said while each label was showing. The samples next to a switch
	// are left out: faders move a moment before the label does.
	want := map[string]liveAuditionSample{
		"Audition ▶ X: as it is":          {slot: 0, db: -6, active: true},
		"Audition ▶ B: the other clip":    {slot: 1, db: -6, active: true},
		"Audition ▶ C: -6 dB, device off": {slot: 0, db: -12, active: false},
	}
	seen := map[string]int{}
	for i, sample := range samples {
		expected, known := want[sample.name]
		if !known || i == 0 || i == len(samples)-1 || samples[i-1].name != sample.name || samples[i+1].name != sample.name {
			continue
		}
		seen[sample.name]++
		if sample.slot != expected.slot || !sameLevel(sample.db, expected.db) || sample.active != expected.active {
			t.Errorf("while %q showed: slot %d, %.3f dB, active %v; want slot %d, %.1f dB, active %v",
				sample.name, sample.slot, sample.db, sample.active, expected.slot, expected.db, expected.active)
		}
	}
	for name := range want {
		if seen[name] < 3 {
			t.Errorf("%q was seen %d times in %d samples; every variant should show for two bars", name, seen[name], len(samples))
		}
	}

	// Everything is back.
	time.Sleep(300 * time.Millisecond)
	after, _ := sampleLiveAudition(t, client, probe, device)
	if after.name != auditionIndicatorName || after.slot != 0 || !sameLevel(after.db, -6) || !after.active {
		t.Errorf("after the audition: %+v, want Audition, slot 0, -6.0 dB, device on", after)
	}
	if now, err := queryClipTriggerQuantization(client); err != nil || now != quantization {
		t.Errorf("clip trigger quantization = %d (%v), want %d back", now, err, quantization)
	}

	// commit writes C in and leaves it.
	committed, err := runAudition(client, time.Sleep, AuditionInput{Variants: liveAuditionVariants(probe, device), Commit: "C"})
	if err != nil {
		t.Fatalf("commit error = %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	after, _ = sampleLiveAudition(t, client, probe, device)
	if !sameLevel(after.db, -12) || after.active || after.slot != 0 {
		t.Errorf("after commit: %+v, want -12.0 dB, device off, slot 0", after)
	}
	if committed.Restored || committed.Committed == nil || len(committed.Committed.Mix) != 1 || !strings.HasPrefix(committed.Committed.Mix[0].VolumeDB, "-12.0") {
		t.Errorf("commit output = %+v (%+v)", committed, committed.Committed)
	}
}

func TestLiveAuditionGivesUpWhenTheListenerStopsPlayback(t *testing.T) {
	client := liveMeasureClient(t)
	probe, device := liveAuditionSetup(t, client)

	done := make(chan error, 1)
	go func() {
		_, err := runAudition(client, time.Sleep, AuditionInput{Variants: liveAuditionVariants(probe, device), Play: []string{"B", "C"}, BarsPerVariant: 4})
		done <- err
	}()
	// Into the second bar of B, the listener has heard enough.
	waitForLive(t, "variant B to show", func() bool { return strings.Contains(liveIndicatorName(t, client), "▶ B") })
	time.Sleep(2500 * time.Millisecond)
	stopped := time.Now()
	if err := client.Send("/live/song/stop_playing"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		var actionableErr *ActionableError
		if !errors.As(err, &actionableErr) || actionableErr.Code != "audition_interrupted" || !strings.Contains(actionableErr.Message, "variant B") {
			t.Fatalf("error = %v, want audition_interrupted naming variant B", err)
		}
		t.Logf("gave up %.1f s after the stop", time.Since(stopped).Seconds())
	case <-time.After(5 * time.Second):
		t.Fatal("the audition kept waiting on a stopped transport")
	}

	time.Sleep(500 * time.Millisecond)
	after, _ := sampleLiveAudition(t, client, probe, device)
	if after.name != auditionIndicatorName || after.slot != 0 || !sameLevel(after.db, -6) || !after.active {
		t.Errorf("after the stop: %+v, want Audition, slot 0 ready to play, -6.0 dB, device on", after)
	}
	playing, err := queryAuditionIsPlaying(client)
	if err != nil || playing {
		t.Errorf("is_playing = %v (%v); the listener stopped playback and it should stay stopped", playing, err)
	}
}

// readAIFFMono reads a PCM AIFF file (what Live records) and folds it to mono.
func readAIFFMono(t *testing.T, path string) ([]float64, float64) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 12 || string(data[:4]) != "FORM" || string(data[8:12]) != "AIFF" {
		t.Fatalf("%s is not a plain AIFF file", path)
	}
	var channels, bits int
	var rate float64
	var pcm []byte
	for i := 12; i+8 <= len(data); {
		id, size := string(data[i:i+4]), int(binary.BigEndian.Uint32(data[i+4:i+8]))
		body := data[i+8 : min(i+8+size, len(data))]
		switch id {
		case "COMM":
			channels, bits = int(binary.BigEndian.Uint16(body[0:2])), int(binary.BigEndian.Uint16(body[6:8]))
			exponent := int(binary.BigEndian.Uint16(body[8:10])&0x7fff) - 16383
			rate = math.Ldexp(float64(binary.BigEndian.Uint64(body[10:18])), exponent-63)
		case "SSND":
			pcm = body[8+int(binary.BigEndian.Uint32(body[0:4])):]
		}
		i += 8 + size + size%2
	}
	if channels == 0 || pcm == nil || (bits != 16 && bits != 24) {
		t.Fatalf("unsupported AIFF: %d channels, %d bits", channels, bits)
	}
	width := bits / 8
	mono := make([]float64, 0, len(pcm)/(width*channels))
	for i := 0; i+width*channels <= len(pcm); i += width * channels {
		sum := 0.0
		for c := 0; c < channels; c++ {
			b := pcm[i+c*width:]
			v := int32(int8(b[0]))<<8 | int32(b[1])
			if width == 3 {
				v = v<<8 | int32(b[2])
			}
			sum += float64(v) / float64(int32(1)<<(bits-1))
		}
		mono = append(mono, sum/float64(channels))
	}
	return mono, rate
}

// The engine sends faders a moment ahead of the bar line so that the downbeat
// already sounds like the new variant. This records Live's output while an
// audition turns a held tone down by 6 dB and up again, and reads off the
// recording when the level really changed.
func TestLiveAuditionFaderLandsJustBeforeTheBarLine(t *testing.T) {
	client := liveMeasureClient(t)
	probe := liveProbeTrack(t, client)
	t.Cleanup(func() { deleteLiveTracksNamed(t, client, auditionIndicatorName) })

	// One note, longer than the whole test: every new note starts with an attack
	// that would look like a level change of its own.
	written, err := writeClipNotation(client, ClipWriteInput{
		TrackIndex: probe, ClipIndex: 2,
		Notation: "clip \"Tone\" bars=16 sig=4/4\n\n  1:1 C5 64 v100\n",
	})
	if err != nil || !written.Verified {
		t.Fatalf("write the tone clip: %v (%+v)", err, written)
	}
	if err := client.Send("/live/clip_slot/fire", int32(probe), int32(2)); err != nil {
		t.Fatal(err)
	}
	waitForLive(t, "the tone to play", func() bool { return livePlayingSlot(t, client, probe) == 2 })

	// Each engine adds its own track at the end of the set. One after the other,
	// or each would take the other's new track for its own.
	if _, err := ensureNamedAudioTrack(client, measureTrackName); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ensureAuditionIndicator(client, time.Sleep); err != nil {
		t.Fatal(err)
	}

	auditionDone := make(chan error, 1)
	go func() {
		_, err := runAudition(client, time.Sleep, AuditionInput{
			Variants: []AuditionVariant{
				{Label: "X", Description: "as it is"},
				{Label: "C", Description: "-6 dB", Mix: []AuditionMix{{TrackIndex: probe, DeltaDB: -6}}},
			},
			Play: []string{"X", "C", "X"}, BarsPerVariant: 2,
		})
		auditionDone <- err
	}()
	take, err := recordResampledPass(client, realRecordDeps(), recordPlan{
		TrackName: measureTrackName,
		Spans:     []recordSpan{{Bars: 8}},
	})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	if err := <-auditionDone; err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}

	mono, rate := readAIFFMono(t, take.FilePaths[0])
	start, end, err := locateWindow(float64(len(mono))/rate, take)
	if err != nil {
		t.Fatal(err)
	}
	const hop = 0.01
	var levels []float64 // dB per 10 ms, from the start of the window
	for at := start; at+hop <= end; at += hop {
		sum := 0.0
		from, to := int(at*rate), int((at+hop)*rate)
		for _, v := range mono[from:to] {
			sum += v * v
		}
		levels = append(levels, 10*math.Log10(sum/float64(to-from)+1e-12))
	}
	loud := append([]float64{}, levels[10:100]...)
	sort.Float64s(loud)
	reference := loud[len(loud)/2]
	t.Logf("window %.3f-%.3f s, tone at %.1f dBFS", start, end, reference)

	// A step counts once the level has stayed across the halfway mark for 100 ms.
	barSec := float64(take.BeatsPerBar) * 60 / take.TempoBPM
	quiet := false
	var steps []float64
	for i := 0; i+10 <= len(levels); i++ {
		crossed := true
		for _, level := range levels[i : i+10] {
			if (level < reference-3) == quiet {
				crossed = false
				break
			}
		}
		if crossed {
			quiet = !quiet
			steps = append(steps, float64(i)*hop)
		}
	}
	if len(steps) != 2 {
		t.Fatalf("level steps at %v s, want one down and one up", steps)
	}
	for _, at := range steps {
		offset := at - math.Round(at/barSec)*barSec
		t.Logf("level step at %.2f s: %.0f ms from the bar line", at, offset*1000)
		if offset > 0 || offset < -0.2 {
			t.Errorf("the fader landed %.0f ms from the bar line, want within the 200 ms before it", offset*1000)
		}
	}

	// Renaming the indicator track must not cost a single buffer: away from the
	// two steps, the tone holds its level in every 10 ms of the recording.
	for i, level := range levels {
		at := float64(i) * hop
		expected := reference
		if at > steps[0] && at < steps[1] {
			expected -= 6
		}
		nearStep := math.Abs(at-steps[0]) < 0.05 || math.Abs(at-steps[1]) < 0.05
		if !nearStep && math.Abs(level-expected) > 1.5 {
			t.Errorf("at %.2f s the tone is at %.1f dBFS, want %.1f: a dropout?", at, level, expected)
		}
	}
}

// A variant that names no clip for a track that was silent has to silence it
// again. The stop goes out a beat ahead of the bar line, like a launch: it must
// not take effect before the line.
func TestLiveAuditionStopsATrackOnTheBarLine(t *testing.T) {
	client := liveMeasureClient(t)
	probe := liveProbeTrack(t, client)
	t.Cleanup(func() { deleteLiveTracksNamed(t, client, auditionIndicatorName) })
	if err := client.Send("/live/song/start_playing"); err != nil {
		t.Fatal(err)
	}
	waitForLive(t, "playback to start", func() bool {
		playing, err := queryAuditionIsPlaying(client)
		return err == nil && playing
	})
	if slot := livePlayingSlot(t, client, probe); slot >= 0 {
		t.Fatalf("the probe track plays slot %d; it should be silent before the audition", slot)
	}

	type outcome struct {
		out AuditionOutput
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := runAudition(client, time.Sleep, AuditionInput{
			Variants: []AuditionVariant{
				{Label: "X", Description: "silent, as it is"},
				{Label: "B", Description: "the blip", Clips: []AuditionClip{{TrackIndex: probe, ClipIndex: 1}}},
			},
			Play: []string{"B", "X"}, BarsPerVariant: 2,
		})
		done <- outcome{out, err}
	}()

	type reading struct {
		beat float64
		slot int
	}
	var readings []reading
	var result outcome
	for running := true; running; {
		select {
		case result = <-done:
			running = false
		default:
			beat, err := queryCurrentSongTime(client)
			if err != nil {
				t.Fatal(err)
			}
			readings = append(readings, reading{beat, livePlayingSlot(t, client, probe)})
		}
	}
	if result.err != nil {
		t.Fatalf("runAudition() error = %v", result.err)
	}
	beatsPerBar := 4.0
	start := float64(result.out.Played[0].StartBar-1) * beatsPerBar
	stop := float64(result.out.Played[1].StartBar-1) * beatsPerBar
	heard := false
	for _, r := range readings {
		if r.beat > start+0.5 && r.beat < stop-0.3 {
			if r.slot != 1 {
				t.Errorf("at beat %.2f the probe plays slot %d; B runs from beat %.0f to %.0f", r.beat, r.slot, start, stop)
			}
			heard = true
		}
	}
	if !heard {
		t.Errorf("no reading fell inside variant B (beats %.0f-%.0f): %v", start, stop, readings)
	}
	time.Sleep(300 * time.Millisecond)
	if slot := livePlayingSlot(t, client, probe); slot >= 0 {
		t.Errorf("after the audition the probe plays slot %d, want silence again", slot)
	}
}
