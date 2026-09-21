package audioanalyze

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeTestWAV puts a 48 kHz, 24-bit source file into a temp dir.
func writeTestWAV(t *testing.T, name string, channels ...[]float64) string {
	t.Helper()
	var buf bytes.Buffer
	if err := writeWAV(&buf, channels, 48000, 24, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// toneThenSilence is `seconds` of a 1 kHz tone followed by `silence` seconds of nothing.
func toneThenSilence(amplitude, seconds, silence float64) []float64 {
	x := resampleTestTone(1000, amplitude, 48000, seconds)
	return append(x, make([]float64, int(silence*48000))...)
}

func readDelivered(t *testing.T, path string) wavAudio {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the delivered file: %v", err)
	}
	audio, format, err := loadAudio(bytes.NewReader(data))
	if err != nil || format != "wav" {
		t.Fatalf("decode the delivered file: %v (%s)", err, format)
	}
	return audio
}

func hasIssue(result FinalizeResult, code string) (FinalizeIssue, bool) {
	for _, issue := range result.Issues {
		if issue.Code == code {
			return issue, true
		}
	}
	return FinalizeIssue{}, false
}

func TestFinalizeConvertsToTheDeliveryFormat(t *testing.T) {
	t.Parallel()

	// Three seconds of tone riding on a DC offset, then two seconds of nothing.
	left := toneThenSilence(0.5, 3, 2)
	for i := range left[:3*48000] {
		left[i] += 0.01
	}
	right := append([]float64(nil), left...)
	source := writeTestWAV(t, "take.wav", left, right)
	before, _ := os.ReadFile(source)

	got, err := Finalize(source, DefaultFinalizeOptions())
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if want := filepath.Join(filepath.Dir(source), "take_44k1_24b.wav"); got.OutputPath != want {
		t.Errorf("output path = %s, want %s", got.OutputPath, want)
	}
	if got.SampleRate != 44100 || got.BitDepth != 24 || got.Channels != 2 {
		t.Errorf("format = %d Hz / %d bit / %d ch, want 44100 / 24 / 2", got.SampleRate, got.BitDepth, got.Channels)
	}
	// The silence goes, but for a fifth of a second.
	if math.Abs(got.DurationSec-3.2) > 0.02 || math.Abs(got.TrimmedSec-1.8) > 0.02 {
		t.Errorf("duration = %.3f s, trimmed = %.3f s; want 3.2 and 1.8", got.DurationSec, got.TrimmedSec)
	}
	// -6 dBFS lifted to the ceiling: about +5 dB, within the 6 dB allowed.
	if math.Abs(got.GainDB-5.0) > 0.1 || math.Abs(got.Delivered.TruePeakDBTP-(-1.0)) > 0.05 {
		t.Errorf("gain = %+.2f dB, delivered true peak = %.2f dBTP; want about +5.0 and -1.0", got.GainDB, got.Delivered.TruePeakDBTP)
	}
	if !got.OK || len(got.Issues) != 0 {
		t.Errorf("ok = %v, issues = %+v; want a clean delivery", got.OK, got.Issues)
	}
	if got.Source.SampleRate != 48000 || math.Abs(got.Source.DurationSec-5) > 0.001 {
		t.Errorf("source = %+v, want 48000 Hz and 5 s", got.Source)
	}

	// What is on disk is what was reported, measured from the file itself.
	audio := readDelivered(t, got.OutputPath)
	if audio.sampleRate != 44100 || audio.channels != 2 || math.Abs(float64(len(audio.left))/44100-got.DurationSec) > 0.001 {
		t.Fatalf("on disk: %d Hz, %d ch, %d frames", audio.sampleRate, audio.channels, len(audio.left))
	}
	mean := 0.0
	for _, v := range audio.left[:44100] {
		mean += v
	}
	if mean /= 44100; math.Abs(mean) > 1e-4 {
		t.Errorf("mean of the first second = %v; the DC offset should be gone", mean)
	}
	if last := audio.left[len(audio.left)-1]; last != 0 {
		t.Errorf("last sample = %v, want the fade to end on zero", last)
	}
	early, late := toneAmplitude(audio.left[:44100], 1000, 44100), toneAmplitude(audio.left[len(audio.left)-44100:], 1000, 44100)
	if late > early*0.5 {
		t.Errorf("the last second is at %.3f of full level %.3f; it should be fading out", late, early)
	}

	if after, _ := os.ReadFile(source); !bytes.Equal(before, after) {
		t.Error("the source file was changed")
	}
}

func TestFinalizeLiftsNoFurtherThanAllowed(t *testing.T) {
	t.Parallel()

	x := toneThenSilence(0.1, 2, 1) // -20 dBFS
	got, err := Finalize(writeTestWAV(t, "quiet.wav", x, x), DefaultFinalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	issue, found := hasIssue(got, "gain_limited")
	if math.Abs(got.GainDB-6) > 1e-9 || !found || issue.Blocking || !got.OK {
		t.Errorf("gain = %v, issues = %+v, ok = %v; want +6 dB, gain_limited as a note, still ok", got.GainDB, got.Issues, got.OK)
	}
	if math.Abs(got.Delivered.TruePeakDBTP-(-14)) > 0.1 {
		t.Errorf("delivered true peak = %.2f, want about -14 dBTP", got.Delivered.TruePeakDBTP)
	}
}

func TestFinalizeTurnsDownWhatIsOverTheCeiling(t *testing.T) {
	t.Parallel()

	x := toneThenSilence(0.99, 2, 1)
	got, err := Finalize(writeTestWAV(t, "hot.wav", x, x), DefaultFinalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got.GainDB >= 0 || got.Delivered.TruePeakDBTP > -0.95 || !got.OK {
		t.Errorf("gain = %+.2f dB, delivered = %.2f dBTP, ok = %v; want it turned down to the ceiling", got.GainDB, got.Delivered.TruePeakDBTP, got.OK)
	}
}

func TestFinalizeFlagsWhatGainCannotFix(t *testing.T) {
	t.Parallel()

	clipped := toneThenSilence(1.6, 2, 1)
	for i, v := range clipped {
		clipped[i] = math.Max(-1, math.Min(1, v)) // flat tops, the way an overloaded master records
	}
	got, err := Finalize(writeTestWAV(t, "clipped.wav", clipped, clipped), DefaultFinalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if issue, found := hasIssue(got, "source_clipped"); !found || !issue.Blocking || got.OK {
		t.Errorf("issues = %+v, ok = %v; want source_clipped to hold the delivery back", got.Issues, got.OK)
	}

	abrupt := resampleTestTone(1000, 0.5, 48000, 2) // the sound runs into the end of the file
	got, err = Finalize(writeTestWAV(t, "abrupt.wav", abrupt, abrupt), DefaultFinalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if issue, found := hasIssue(got, "ending_cut_off"); !found || !issue.Blocking || got.OK {
		t.Errorf("issues = %+v, ok = %v; want ending_cut_off to hold the delivery back", got.Issues, got.OK)
	}
	if got.TrimmedSec != 0 {
		t.Errorf("trimmed = %v s; there was no silence to trim", got.TrimmedSec)
	}

	late := append(make([]float64, 24000), toneThenSilence(0.5, 2, 1)...)
	got, err = Finalize(writeTestWAV(t, "late.wav", late, late), DefaultFinalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if issue, found := hasIssue(got, "leading_silence"); !found || issue.Blocking || !got.OK {
		t.Errorf("issues = %+v, ok = %v; want leading_silence as a note", got.Issues, got.OK)
	}
}

func TestFinalizeNeverOverwritesUnlessTold(t *testing.T) {
	t.Parallel()

	x := toneThenSilence(0.5, 1, 1)
	source := writeTestWAV(t, "song.wav", x, x)
	opts := DefaultFinalizeOptions()
	if _, err := Finalize(source, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Finalize(source, opts); !errors.Is(err, ErrOutputExists) {
		t.Errorf("second run: error = %v, want ErrOutputExists", err)
	}
	opts.Overwrite = true
	if _, err := Finalize(source, opts); err != nil {
		t.Errorf("with overwrite: error = %v", err)
	}
	// The source is never a valid destination, whatever the flag says.
	opts.OutputPath = source
	if _, err := Finalize(source, opts); !errors.Is(err, ErrOutputExists) {
		t.Errorf("output = source: error = %v, want ErrOutputExists", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(source)); len(entries) != 2 {
		t.Errorf("files in the folder = %d, want the source and one delivery (no temp files left)", len(entries))
	}
}

func TestFinalizeKeepsMonoAndWritesSixteenBit(t *testing.T) {
	t.Parallel()

	source := writeTestWAV(t, "mono.wav", toneThenSilence(0.5, 1, 1))
	opts := DefaultFinalizeOptions()
	opts.BitDepth, opts.SampleRate = 16, 48000
	got, err := Finalize(source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channels != 1 || got.BitDepth != 16 || got.SampleRate != 48000 || filepath.Base(got.OutputPath) != "mono_48k_16b.wav" {
		t.Errorf("got %d ch / %d bit / %d Hz at %s", got.Channels, got.BitDepth, got.SampleRate, got.OutputPath)
	}
	if audio := readDelivered(t, got.OutputPath); audio.channels != 1 || audio.sampleRate != 48000 {
		t.Errorf("on disk: %d ch at %d Hz", audio.channels, audio.sampleRate)
	}
}

func TestFinalizeReadsAIFFAndChecksItsOptions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "take.aif")
	if err := os.WriteFile(path, stereoToneAIFF(1000, 0.5, 48000, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := DefaultFinalizeOptions()
	opts.TrimTail, opts.FadeOutSec = false, 0
	got, err := Finalize(path, opts)
	if err != nil {
		t.Fatalf("Finalize(aiff) error = %v", err)
	}
	if math.Abs(got.DurationSec-2) > 0.001 || got.TrimmedSec != 0 {
		t.Errorf("duration = %v, trimmed = %v; nothing was to be cut", got.DurationSec, got.TrimmedSec)
	}

	for name, change := range map[string]func(*FinalizeOptions){
		"rate":     func(o *FinalizeOptions) { o.SampleRate = 22050 },
		"bits":     func(o *FinalizeOptions) { o.BitDepth = 32 },
		"ceiling":  func(o *FinalizeOptions) { o.CeilingDBTP = 0.5 },
		"max gain": func(o *FinalizeOptions) { o.MaxGainDB = 40 },
		"fade":     func(o *FinalizeOptions) { o.FadeOutSec = -1 },
		"not wav":  func(o *FinalizeOptions) { o.OutputPath = filepath.Join(filepath.Dir(path), "out.mp3") },
	} {
		bad := DefaultFinalizeOptions()
		change(&bad)
		if _, err := Finalize(path, bad); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestFinalizeLeavesTheSubBassAlone(t *testing.T) {
	t.Parallel()

	// Taking DC out must not take the bottom of an 808 with it.
	x := append(resampleTestTone(40, 0.5, 48000, 3), make([]float64, 48000)...)
	opts := DefaultFinalizeOptions()
	opts.FadeOutSec = 0
	got, err := Finalize(writeTestWAV(t, "sub.wav", x, x), opts)
	if err != nil {
		t.Fatal(err)
	}
	audio := readDelivered(t, got.OutputPath)
	want := 0.5 * math.Pow(10, got.GainDB/20)
	if loss := 20 * math.Log10(toneAmplitude(audio.left[:3*44100], 40, 44100)/want); math.Abs(loss) > 0.2 {
		t.Errorf("40 Hz comes out %+.2f dB off, want within 0.2 dB", loss)
	}
}
