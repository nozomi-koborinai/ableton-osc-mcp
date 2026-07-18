package audioanalyze

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxFileBytes      = 80 << 20 // 80 MiB
	maxChunkBytes     = 1 << 20  // cap non-data chunk allocation (guards bogus sizes)
	minBPM            = 70.0
	maxBPM            = 180.0
	envelopeHop       = 512
	maxAnalyzeSamples = 44100 * 60 // analyze at most 60s of audio
)

type Result struct {
	Path              string  `json:"path"`
	Format            string  `json:"format"`
	DurationSec       float64 `json:"duration_sec"`
	SampleRate        int     `json:"sample_rate"`
	Channels          int     `json:"channels"`
	PeakLevel         float64 `json:"peak_level"`
	RMSLevel          float64 `json:"rms_level"`
	EstimatedBPM      float64 `json:"estimated_bpm"`
	BPMConfidence     float64 `json:"bpm_confidence"`
	OnsetCount        int     `json:"onset_count"`
	SuggestedWarpMode string  `json:"suggested_warp_mode"`
	LengthBarsAtBPM   float64 `json:"length_bars_at_project_tempo,omitempty"`
	Note              string  `json:"note"`
}

// AnalyzeFile analyzes a local WAV file already present on disk. It never
// downloads or writes audio; callers must supply audio they have rights to use.
func AnalyzeFile(path string, projectTempo float64) (Result, error) {
	abs, err := validateLocalAudioPath(path)
	if err != nil {
		return Result{}, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Result{}, fmt.Errorf("stat audio file: %w", err)
	}
	if info.IsDir() {
		return Result{}, errors.New("path must be a file")
	}
	if info.Size() <= 0 {
		return Result{}, errors.New("audio file is empty")
	}
	if info.Size() > maxFileBytes {
		return Result{}, fmt.Errorf("audio file too large (%d bytes); max is %d", info.Size(), maxFileBytes)
	}

	f, err := os.Open(abs)
	if err != nil {
		return Result{}, fmt.Errorf("open audio file: %w", err)
	}
	defer func() { _ = f.Close() }()

	out, err := analyzeWAVStream(f, projectTempo)
	if err != nil {
		return Result{}, err
	}
	out.Path = abs
	out.Note = "Local-file analysis only. No download, transcription, or note extraction. Use results to place/warp a sample you already have rights to use."
	return out, nil
}

// analyzeWAVStream decodes WAV audio from r and computes sampling-oriented
// metadata. Path and Note are left for the caller to fill in per source.
func analyzeWAVStream(r io.Reader, projectTempo float64) (Result, error) {
	mono, sampleRate, channels, err := loadMonoWAV(io.LimitReader(r, maxFileBytes+1))
	if err != nil {
		return Result{}, err
	}
	if len(mono) == 0 || sampleRate <= 0 {
		return Result{}, errors.New("no audio samples decoded")
	}

	duration := float64(len(mono)) / float64(sampleRate)
	peak, rms := levels(mono)
	analyze := mono
	if len(analyze) > maxAnalyzeSamples {
		analyze = analyze[:maxAnalyzeSamples]
	}
	bpm, confidence := estimateBPM(analyze, sampleRate)
	onsets := countOnsets(analyze, sampleRate)
	warpMode := "beats"
	if duration >= 8 || onsets < 8 {
		warpMode = "complex"
	}

	out := Result{
		Format:            "wav",
		DurationSec:       duration,
		SampleRate:        sampleRate,
		Channels:          channels,
		PeakLevel:         peak,
		RMSLevel:          rms,
		EstimatedBPM:      bpm,
		BPMConfidence:     confidence,
		OnsetCount:        onsets,
		SuggestedWarpMode: warpMode,
	}
	if projectTempo > 0 && duration > 0 {
		beats := duration * (projectTempo / 60)
		out.LengthBarsAtBPM = beats / 4
	}
	return out, nil
}

func validateLocalAudioPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "//") {
		return "", errors.New("remote URLs are not supported; provide a local file path to audio you already have")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".wav" {
		return "", errors.New("only local .wav files are supported in this version")
	}
	return filepath.Clean(path), nil
}

func loadMonoWAV(f io.Reader) ([]float64, int, int, error) {
	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return nil, 0, 0, fmt.Errorf("read wav header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return nil, 0, 0, errors.New("not a RIFF/WAVE file")
	}

	var (
		audioFormat   uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		data          []byte
	)

	for {
		var chunkHeader [8]byte
		if _, err := io.ReadFull(f, chunkHeader[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, 0, 0, err
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		// Streaming encoders (e.g. ffmpeg writing to a pipe) cannot seek back to
		// patch the data chunk size, so it holds a placeholder. Read the data
		// chunk to EOF instead of trusting the declared size.
		if chunkID == "data" {
			payload, err := io.ReadAll(f)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("read data chunk: %w", err)
			}
			data = payload
			break
		}

		if chunkSize > maxChunkBytes {
			return nil, 0, 0, fmt.Errorf("%s chunk too large (%d bytes)", chunkID, chunkSize)
		}
		payload := make([]byte, chunkSize)
		if _, err := io.ReadFull(f, payload); err != nil {
			return nil, 0, 0, fmt.Errorf("read %s chunk: %w", chunkID, err)
		}
		// Chunks are word-aligned.
		if chunkSize%2 == 1 {
			var pad [1]byte
			_, _ = f.Read(pad[:])
		}
		if chunkID == "fmt " {
			if len(payload) < 16 {
				return nil, 0, 0, errors.New("invalid fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(payload[0:2])
			channels = binary.LittleEndian.Uint16(payload[2:4])
			sampleRate = binary.LittleEndian.Uint32(payload[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(payload[14:16])
		}
	}
	if len(data) == 0 || channels == 0 || sampleRate == 0 {
		return nil, 0, 0, errors.New("wav missing fmt/data")
	}
	if audioFormat != 1 && audioFormat != 3 {
		return nil, 0, 0, fmt.Errorf("unsupported wav format code %d (need PCM or IEEE float)", audioFormat)
	}

	mono, err := decodeMono(data, int(channels), int(bitsPerSample), audioFormat)
	if err != nil {
		return nil, 0, 0, err
	}
	return mono, int(sampleRate), int(channels), nil
}

func decodeMono(data []byte, channels, bitsPerSample int, audioFormat uint16) ([]float64, error) {
	if channels < 1 {
		return nil, errors.New("invalid channel count")
	}
	switch {
	case audioFormat == 1 && bitsPerSample == 16:
		frame := 2 * channels
		if len(data) < frame {
			return nil, errors.New("wav data too short")
		}
		n := len(data) / frame
		out := make([]float64, n)
		for i := 0; i < n; i++ {
			var sum float64
			for ch := 0; ch < channels; ch++ {
				off := i*frame + ch*2
				sample := int16(binary.LittleEndian.Uint16(data[off : off+2]))
				sum += float64(sample) / 32768.0
			}
			out[i] = sum / float64(channels)
		}
		return out, nil
	case audioFormat == 1 && bitsPerSample == 24:
		frame := 3 * channels
		if len(data) < frame {
			return nil, errors.New("wav data too short")
		}
		n := len(data) / frame
		out := make([]float64, n)
		for i := 0; i < n; i++ {
			var sum float64
			for ch := 0; ch < channels; ch++ {
				off := i*frame + ch*3
				v := int32(data[off]) | int32(data[off+1])<<8 | int32(data[off+2])<<16
				if v&0x800000 != 0 {
					v |= ^0xFFFFFF
				}
				sum += float64(v) / 8388608.0
			}
			out[i] = sum / float64(channels)
		}
		return out, nil
	case audioFormat == 3 && bitsPerSample == 32:
		frame := 4 * channels
		if len(data) < frame {
			return nil, errors.New("wav data too short")
		}
		n := len(data) / frame
		out := make([]float64, n)
		for i := 0; i < n; i++ {
			var sum float64
			for ch := 0; ch < channels; ch++ {
				off := i*frame + ch*4
				bits := binary.LittleEndian.Uint32(data[off : off+4])
				sum += float64(math.Float32frombits(bits))
			}
			out[i] = sum / float64(channels)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported wav encoding: format=%d bits=%d", audioFormat, bitsPerSample)
	}
}

func levels(samples []float64) (peak, rms float64) {
	if len(samples) == 0 {
		return 0, 0
	}
	var sumSq float64
	for _, s := range samples {
		a := math.Abs(s)
		if a > peak {
			peak = a
		}
		sumSq += s * s
	}
	rms = math.Sqrt(sumSq / float64(len(samples)))
	return peak, rms
}

func estimateBPM(samples []float64, sampleRate int) (float64, float64) {
	if len(samples) < sampleRate || sampleRate <= 0 {
		return 0, 0
	}
	env := energyEnvelope(samples, envelopeHop)
	if len(env) < 8 {
		return 0, 0
	}
	// First-order difference emphasizes onsets.
	diff := make([]float64, len(env))
	for i := 1; i < len(env); i++ {
		d := env[i] - env[i-1]
		if d > 0 {
			diff[i] = d
		}
	}

	hopSec := float64(envelopeHop) / float64(sampleRate)
	minLag := int(math.Floor((60.0 / maxBPM) / hopSec))
	maxLag := int(math.Ceil((60.0 / minBPM) / hopSec))
	if minLag < 1 {
		minLag = 1
	}
	if maxLag >= len(diff) {
		maxLag = len(diff) - 1
	}
	if minLag >= maxLag {
		return 0, 0
	}

	bestLag := minLag
	bestCorr := -1.0
	secondCorr := -1.0
	for lag := minLag; lag <= maxLag; lag++ {
		var corr, energy float64
		n := len(diff) - lag
		for i := 0; i < n; i++ {
			corr += diff[i] * diff[i+lag]
			energy += diff[i] * diff[i]
		}
		if energy <= 1e-12 {
			continue
		}
		corr /= energy
		if corr > bestCorr {
			secondCorr = bestCorr
			bestCorr = corr
			bestLag = lag
		} else if corr > secondCorr {
			secondCorr = corr
		}
	}
	if bestCorr <= 0 {
		return 0, 0
	}
	bpm := 60.0 / (float64(bestLag) * hopSec)
	// Prefer common dance tempos when a double/half is equally plausible.
	bpm = normalizeBPM(bpm)
	confidence := bestCorr
	if secondCorr > 0 {
		confidence = math.Max(0, math.Min(1, (bestCorr-secondCorr)/math.Max(bestCorr, 1e-9)))
		confidence = math.Max(confidence, math.Min(1, bestCorr))
	}
	return round1(bpm), round2(confidence)
}

func normalizeBPM(bpm float64) float64 {
	for bpm < minBPM && bpm*2 <= maxBPM {
		bpm *= 2
	}
	for bpm > maxBPM && bpm/2 >= minBPM {
		bpm /= 2
	}
	return bpm
}

func energyEnvelope(samples []float64, hop int) []float64 {
	if hop < 1 {
		hop = 512
	}
	n := len(samples) / hop
	out := make([]float64, 0, n)
	for i := 0; i+hop <= len(samples); i += hop {
		var sum float64
		for _, s := range samples[i : i+hop] {
			sum += s * s
		}
		out = append(out, math.Sqrt(sum/float64(hop)))
	}
	return out
}

func countOnsets(samples []float64, sampleRate int) int {
	env := energyEnvelope(samples, envelopeHop)
	if len(env) < 3 {
		return 0
	}
	var mean float64
	for _, v := range env {
		mean += v
	}
	mean /= float64(len(env))
	threshold := mean * 1.5
	count := 0
	armed := true
	minGap := int(math.Round(0.08 / (float64(envelopeHop) / float64(sampleRate)))) // ~80ms
	if minGap < 1 {
		minGap = 1
	}
	last := -minGap
	for i := 1; i < len(env)-1; i++ {
		if armed && env[i] > threshold && env[i] >= env[i-1] && env[i] >= env[i+1] && i-last >= minGap {
			count++
			last = i
			armed = false
		}
		if env[i] < threshold*0.8 {
			armed = true
		}
	}
	return count
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
