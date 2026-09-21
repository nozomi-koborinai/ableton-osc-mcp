package audioanalyze

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxFinalizeBytes = 256 << 20 // about a quarter of an hour of 48 kHz / 24-bit stereo

	finalizeDCCutoffHz     = 5.0
	finalizeTailFloorDB    = -70.0 // where a decaying tail counts as over
	finalizeTailKeepSec    = 0.2   // how much is kept after that point
	finalizeEndingWindow   = 0.1   // seconds at the end of the source that must have decayed
	finalizeEndingFloorDB  = -50.0
	finalizeLeadingFloorDB = -60.0
	finalizeLeadingMaxSec  = 0.05
	finalizeClipLevel      = 0.999 // flat runs at this level or above are clipping
	finalizeClipRun        = 3
	finalizeCeilingSlackDB = 0.05
)

// ErrOutputExists says Finalize would have had to replace a file: the delivery
// exists already and Overwrite was not set, or the destination is the source.
var ErrOutputExists = errors.New("output file exists")

// FinalizeOptions says what a delivery looks like.
type FinalizeOptions struct {
	OutputPath  string // default: next to the source, <name>_44k1_24b.wav
	SampleRate  int    // 44100 or 48000
	BitDepth    int    // 16 or 24
	CeilingDBTP float64
	MaxGainDB   float64 // how far a quiet source may be lifted towards the ceiling
	TrimTail    bool    // cut the silence after the last sound
	FadeOutSec  float64
	Overwrite   bool
}

// DefaultFinalizeOptions is a 44.1 kHz / 24-bit file peaking at -1 dBTP.
func DefaultFinalizeOptions() FinalizeOptions {
	return FinalizeOptions{SampleRate: 44100, BitDepth: 24, CeilingDBTP: -1, MaxGainDB: 6, TrimTail: true, FadeOutSec: 1.5}
}

// AudioLevels is what a file measures.
type AudioLevels struct {
	TruePeakDBTP   float64
	LUFSIntegrated float64
	SamplePeakDBFS float64
	SampleRate     int
	DurationSec    float64
}

// FinalizeIssue is one finding of the checks. A blocking issue means the file
// should not be delivered as it is.
type FinalizeIssue struct {
	Code     string
	Message  string
	Blocking bool
}

type FinalizeResult struct {
	OutputPath  string
	SampleRate  int
	BitDepth    int
	Channels    int
	DurationSec float64
	GainDB      float64
	TrimmedSec  float64
	Source      AudioLevels
	Delivered   AudioLevels // measured from the written file, not from memory
	Issues      []FinalizeIssue
	OK          bool
}

// Finalize turns a recording into a delivery: the silence after the last sound
// cut, DC taken out, a fade-out, the sample rate converted, the level set against a
// true-peak ceiling with gain alone (no limiting: loudness is made in Live), and
// written as WAV. The source file is never touched. The result reports what the
// written file measures and what speaks against delivering it.
func Finalize(path string, opts FinalizeOptions) (FinalizeResult, error) {
	abs, err := validateLocalAudioPath(path)
	if err != nil {
		return FinalizeResult{}, err
	}
	output, err := opts.resolve(abs)
	if err != nil {
		return FinalizeResult{}, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("stat audio file: %w", err)
	}
	if info.IsDir() || info.Size() <= 0 {
		return FinalizeResult{}, errors.New("path must be a non-empty audio file")
	}
	if info.Size() > maxFinalizeBytes {
		return FinalizeResult{}, fmt.Errorf("audio file too large (%d bytes); max is %d", info.Size(), maxFinalizeBytes)
	}
	if existing, err := os.Stat(output); err == nil {
		if os.SameFile(info, existing) {
			return FinalizeResult{}, fmt.Errorf("%w: the destination is the source file itself", ErrOutputExists)
		}
		if !opts.Overwrite {
			return FinalizeResult{}, fmt.Errorf("%w: %s", ErrOutputExists, output)
		}
	}

	channels, sourceRate, err := decodeForDelivery(abs)
	if err != nil {
		return FinalizeResult{}, err
	}
	result := FinalizeResult{OutputPath: output, SampleRate: opts.SampleRate, BitDepth: opts.BitDepth, Channels: len(channels)}
	result.Source = measureLevels(channels, sourceRate)
	result.Issues = checkSource(channels, sourceRate)

	// Where the sound ends is read off the recording as it is: taking DC out
	// first would leave the filter's own settling in the silence after it.
	end := len(channels[0])
	if opts.TrimTail {
		if end, err = tailEnd(channels, sourceRate); err != nil {
			return FinalizeResult{}, err
		}
	}
	result.TrimmedSec = float64(len(channels[0])-end) / float64(sourceRate)
	blockDC(channels, sourceRate)
	for i := range channels {
		channels[i] = channels[i][:end]
	}
	fadeOut(channels, int(opts.FadeOutSec*float64(sourceRate)))

	for i := range channels {
		if channels[i], err = resampleRational(channels[i], sourceRate, opts.SampleRate); err != nil {
			return FinalizeResult{}, err
		}
	}

	// Gain alone, measured after the conversion: resampling moves inter-sample peaks.
	wanted := opts.CeilingDBTP - truePeakDBTP(channels)
	result.GainDB = math.Min(opts.MaxGainDB, wanted)
	if wanted > opts.MaxGainDB+1e-9 {
		result.Issues = append(result.Issues, FinalizeIssue{Code: "gain_limited",
			Message: fmt.Sprintf("reaching %.1f dBTP would take %+.1f dB; lifted by the %.1f dB allowed", opts.CeilingDBTP, wanted, opts.MaxGainDB)})
	}
	gain := math.Pow(10, result.GainDB/20)
	for _, ch := range channels {
		for i := range ch {
			ch[i] *= gain
		}
	}

	if err := writeDelivery(output, channels, opts); err != nil {
		return FinalizeResult{}, err
	}

	// What counts is the file, so the file is what gets measured.
	written, writtenRate, err := decodeForDelivery(output)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("read back the delivery: %w", err)
	}
	result.Delivered = measureLevels(written, writtenRate)
	result.DurationSec = result.Delivered.DurationSec
	if result.Delivered.TruePeakDBTP > opts.CeilingDBTP+finalizeCeilingSlackDB {
		result.Issues = append(result.Issues, FinalizeIssue{Code: "true_peak_over_ceiling", Blocking: true,
			Message: fmt.Sprintf("the written file peaks at %.2f dBTP, over the ceiling of %.1f dBTP", result.Delivered.TruePeakDBTP, opts.CeilingDBTP)})
	}
	result.OK = true
	for _, issue := range result.Issues {
		result.OK = result.OK && !issue.Blocking
	}
	return result, nil
}

// resolve checks the options and names the output file.
func (o FinalizeOptions) resolve(source string) (string, error) {
	if o.SampleRate != 44100 && o.SampleRate != 48000 {
		return "", fmt.Errorf("sample_rate must be 44100 or 48000, got %d", o.SampleRate)
	}
	if o.BitDepth != 16 && o.BitDepth != 24 {
		return "", fmt.Errorf("bit_depth must be 16 or 24, got %d", o.BitDepth)
	}
	if math.IsNaN(o.CeilingDBTP) || o.CeilingDBTP < -6 || o.CeilingDBTP > 0 {
		return "", fmt.Errorf("ceiling_dbtp must be between -6 and 0, got %v", o.CeilingDBTP)
	}
	if math.IsNaN(o.MaxGainDB) || o.MaxGainDB < 0 || o.MaxGainDB > 12 {
		return "", fmt.Errorf("max_gain_db must be between 0 and 12, got %v", o.MaxGainDB)
	}
	if math.IsNaN(o.FadeOutSec) || o.FadeOutSec < 0 || o.FadeOutSec > 10 {
		return "", fmt.Errorf("fade_out_sec must be between 0 and 10, got %v", o.FadeOutSec)
	}
	output := strings.TrimSpace(o.OutputPath)
	if output == "" {
		rate := map[int]string{44100: "44k1", 48000: "48k"}[o.SampleRate]
		stem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
		return filepath.Join(filepath.Dir(source), fmt.Sprintf("%s_%s_%db.wav", stem, rate, o.BitDepth)), nil
	}
	if !filepath.IsAbs(output) || strings.ToLower(filepath.Ext(output)) != ".wav" {
		return "", errors.New("output_path must be an absolute path ending in .wav")
	}
	return filepath.Clean(output), nil
}

// decodeForDelivery reads a file into one slice per channel (one or two).
func decodeForDelivery(path string) ([][]float64, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open audio file: %w", err)
	}
	defer func() { _ = f.Close() }()
	audio, _, err := loadAudio(io.LimitReader(f, maxFinalizeBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if audio.sampleRate <= 0 || len(audio.mono) == 0 {
		return nil, 0, errors.New("no audio samples decoded")
	}
	if audio.channels < 2 {
		return [][]float64{append([]float64(nil), audio.mono...)}, audio.sampleRate, nil
	}
	return [][]float64{append([]float64(nil), audio.left...), append([]float64(nil), audio.right...)}, audio.sampleRate, nil
}

func measureLevels(channels [][]float64, sampleRate int) AudioLevels {
	return AudioLevels{
		TruePeakDBTP:   truePeakDBTP(channels),
		LUFSIntegrated: integratedLUFS(channels, sampleRate),
		SamplePeakDBFS: amplitudeDB(peakOf(channels, 0, len(channels[0]))),
		SampleRate:     sampleRate,
		DurationSec:    float64(len(channels[0])) / float64(sampleRate),
	}
}

// peakOf is the largest absolute sample of any channel in [from, to).
func peakOf(channels [][]float64, from, to int) float64 {
	peak := 0.0
	for _, ch := range channels {
		for _, v := range ch[from:to] {
			peak = math.Max(peak, math.Abs(v))
		}
	}
	return peak
}

// checkSource looks for what no processing here can repair.
func checkSource(channels [][]float64, sampleRate int) []FinalizeIssue {
	var issues []FinalizeIssue
	n := len(channels[0])

	// Clipping is a flat top: the same full-scale value several samples in a row.
	// A clean peak that merely touches full scale keeps moving from sample to sample.
	for _, ch := range channels {
		run, clipped := 1, false
		for i := 1; i < len(ch) && !clipped; i++ {
			if ch[i] == ch[i-1] && math.Abs(ch[i]) >= finalizeClipLevel {
				run++
				clipped = run >= finalizeClipRun
			} else {
				run = 1
			}
		}
		if clipped {
			issues = append(issues, FinalizeIssue{Code: "source_clipped", Blocking: true,
				Message: "the recording is clipped (flat tops at full scale); gain cannot undo that. Turn the master down in Live and record again"})
			break
		}
	}

	window := int(finalizeEndingWindow * float64(sampleRate))
	if window > n {
		window = n
	}
	if ending := amplitudeDB(peakOf(channels, n-window, n)); ending > finalizeEndingFloorDB {
		issues = append(issues, FinalizeIssue{Code: "ending_cut_off", Blocking: true,
			Message: fmt.Sprintf("the recording ends while the sound is still at %.1f dBFS; record more tail (tail_bars) so that it can ring out", ending)})
	}

	floor := math.Pow(10, finalizeLeadingFloorDB/20)
	first := n
	for i := 0; i < n && first == n; i++ {
		for _, ch := range channels {
			if math.Abs(ch[i]) > floor {
				first = i
			}
		}
	}
	if lead := float64(first) / float64(sampleRate); first < n && lead > finalizeLeadingMaxSec {
		issues = append(issues, FinalizeIssue{Code: "leading_silence",
			Message: fmt.Sprintf("the file starts with %.2f s of silence", lead)})
	}
	return issues
}

// blockDC takes out DC with a first-order high-pass at 5 Hz, run forwards and
// then backwards so that no phase moves (-0.1 dB at 40 Hz). Subtracting the
// mean of the file would not do: DC that is only there while something plays
// (an asymmetrically driven 808) leaves the opposite offset in every silence.
func blockDC(channels [][]float64, sampleRate int) {
	r := math.Exp(-2 * math.Pi * finalizeDCCutoffHz / float64(sampleRate))
	for _, ch := range channels {
		for pass := 0; pass < 2; pass++ {
			// Start as if the first value had been there forever: no step, no click.
			prevIn, prevOut := ch[0], 0.0
			for i, v := range ch {
				prevOut = v - prevIn + r*prevOut
				prevIn = v
				ch[i] = prevOut
			}
			for i, j := 0, len(ch)-1; i < j; i, j = i+1, j-1 {
				ch[i], ch[j] = ch[j], ch[i]
			}
		}
	}
}

// tailEnd is where the file may end: a moment after the last sample above the floor.
func tailEnd(channels [][]float64, sampleRate int) (int, error) {
	floor := math.Pow(10, finalizeTailFloorDB/20)
	n := len(channels[0])
	for i := n - 1; i >= 0; i-- {
		for _, ch := range channels {
			if math.Abs(ch[i]) > floor {
				return min(n, i+1+int(finalizeTailKeepSec*float64(sampleRate))), nil
			}
		}
	}
	return 0, errors.New("the file is silent: nothing above -70 dBFS")
}

// fadeOut lowers the last n frames to zero along a squared ramp.
func fadeOut(channels [][]float64, n int) {
	if n = min(n, len(channels[0])); n < 2 {
		return
	}
	start := len(channels[0]) - n
	for _, ch := range channels {
		for i := 0; i < n; i++ {
			ramp := float64(n-1-i) / float64(n-1)
			ch[start+i] *= ramp * ramp
		}
	}
}

// writeDelivery writes next to the destination and renames, so a delivery that
// exists is either the old one or the new one, never half of one.
func writeDelivery(output string, channels [][]float64, opts FinalizeOptions) error {
	temp, err := os.CreateTemp(filepath.Dir(output), ".delivery-*.wav")
	if err != nil {
		return fmt.Errorf("create the delivery: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	var dither func() float64
	if opts.BitDepth == 16 {
		noise := rand.New(rand.NewSource(1)) // the same input gives the same file
		dither = func() float64 { return noise.Float64() - noise.Float64() }
	}
	if err := writeWAV(temp, channels, opts.SampleRate, opts.BitDepth, dither); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write the delivery: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("write the delivery: %w", err)
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return fmt.Errorf("write the delivery: %w", err)
	}
	if err := os.Rename(tempPath, output); err != nil {
		return fmt.Errorf("put the delivery in place: %w", err)
	}
	return nil
}
