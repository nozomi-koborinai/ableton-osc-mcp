package tools

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	defaultAutogainTargetLevel  = 0.45
	defaultAutogainTolerance    = 0.05
	defaultAutogainMaxIters     = 6
	defaultAutogainSettleMs     = 150
	defaultAutogainMeterSamples = 3
	autogainSilentThreshold     = 0.01
	autogainMaxVolumeStep       = 0.12
)

type AutogainTracksInput struct {
	TrackIndices  []int    `json:"track_indices,omitempty" jsonschema:"description=Tracks to adjust; omit to include all regular tracks"`
	TargetLevel   *float64 `json:"target_level,omitempty" jsonschema:"description=Target output meter level 0-1 (default 0.45),minimum=0.05,maximum=0.95"`
	Tolerance     *float64 `json:"tolerance,omitempty" jsonschema:"description=Acceptable meter error (default 0.05),minimum=0.01,maximum=0.3"`
	MaxIterations *int     `json:"max_iterations,omitempty" jsonschema:"description=Max adjust loops per track (default 6),minimum=1,maximum=20"`
	SettleMs      *int     `json:"settle_ms,omitempty" jsonschema:"description=Wait after each volume change in ms (default 150),minimum=0,maximum=2000"`
}

type AutogainTrackResult struct {
	TrackIndex   int     `json:"track_index"`
	Status       string  `json:"status" jsonschema:"description=ok, silent, unchanged, capped, or partial"`
	VolumeBefore float64 `json:"volume_before"`
	VolumeAfter  float64 `json:"volume_after"`
	MeterBefore  float64 `json:"meter_before"`
	MeterAfter   float64 `json:"meter_after"`
	Iterations   int     `json:"iterations"`
}

type AutogainTracksOutput struct {
	TargetLevel float64               `json:"target_level"`
	Tolerance   float64               `json:"tolerance"`
	Results     []AutogainTrackResult `json:"results"`
}

type autogainClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

type sleeperFunc func(time.Duration)

func NewAbletonAutogainTracks(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_autogain_tracks",
		"Ableton Live: iteratively adjust track volumes toward a target meter level while audio is playing",
		func(_ *ai.ToolContext, input AutogainTracksInput) (AutogainTracksOutput, error) {
			return autogainTracks(client, input, time.Sleep)
		},
	)
}

func autogainTracks(client autogainClient, input AutogainTracksInput, sleep sleeperFunc) (AutogainTracksOutput, error) {
	target := defaultAutogainTargetLevel
	if input.TargetLevel != nil {
		target = *input.TargetLevel
	}
	tolerance := defaultAutogainTolerance
	if input.Tolerance != nil {
		tolerance = *input.Tolerance
	}
	maxIters := defaultAutogainMaxIters
	if input.MaxIterations != nil {
		maxIters = *input.MaxIterations
	}
	settleMs := defaultAutogainSettleMs
	if input.SettleMs != nil {
		settleMs = *input.SettleMs
	}

	if target < 0.05 || target > 0.95 {
		return AutogainTracksOutput{}, errors.New("target_level must be between 0.05 and 0.95")
	}
	if tolerance < 0.01 || tolerance > 0.3 {
		return AutogainTracksOutput{}, errors.New("tolerance must be between 0.01 and 0.3")
	}
	if maxIters < 1 || maxIters > 20 {
		return AutogainTracksOutput{}, errors.New("max_iterations must be between 1 and 20")
	}
	if settleMs < 0 || settleMs > 2000 {
		return AutogainTracksOutput{}, errors.New("settle_ms must be between 0 and 2000")
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	indices, err := resolveAutogainTracks(client, input.TrackIndices)
	if err != nil {
		return AutogainTracksOutput{}, err
	}

	results := make([]AutogainTrackResult, 0, len(indices))
	for _, trackIndex := range indices {
		result, err := autogainOneTrack(client, trackIndex, target, tolerance, maxIters, settleMs, sleep)
		if err != nil {
			return AutogainTracksOutput{}, fmt.Errorf("track %d: %w", trackIndex, err)
		}
		results = append(results, result)
	}

	return AutogainTracksOutput{
		TargetLevel: target,
		Tolerance:   tolerance,
		Results:     results,
	}, nil
}

func resolveAutogainTracks(client autogainClient, requested []int) ([]int, error) {
	if len(requested) > 0 {
		out := make([]int, 0, len(requested))
		seen := make(map[int]bool, len(requested))
		for _, idx := range requested {
			if idx < 0 {
				return nil, errors.New("track_indices must be >= 0")
			}
			if seen[idx] {
				continue
			}
			seen[idx] = true
			out = append(out, idx)
		}
		return out, nil
	}

	namesRes, err := client.Query("/live/song/get/track_names")
	if err != nil {
		return nil, fmt.Errorf("list tracks: %w", err)
	}
	n := len(namesRes)
	if n == 0 {
		return nil, errors.New("no tracks available")
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = i
	}
	return out, nil
}

func autogainOneTrack(
	client autogainClient,
	trackIndex int,
	target, tolerance float64,
	maxIters, settleMs int,
	sleep sleeperFunc,
) (AutogainTrackResult, error) {
	volume, err := queryTrackVolume(client, trackIndex)
	if err != nil {
		return AutogainTrackResult{}, err
	}
	meterBefore, err := sampleTrackMeter(client, trackIndex, defaultAutogainMeterSamples)
	if err != nil {
		return AutogainTrackResult{}, err
	}

	result := AutogainTrackResult{
		TrackIndex:   trackIndex,
		VolumeBefore: volume,
		MeterBefore:  meterBefore,
		VolumeAfter:  volume,
		MeterAfter:   meterBefore,
	}

	if meterBefore < autogainSilentThreshold {
		result.Status = "silent"
		return result, nil
	}
	if math.Abs(meterBefore-target) <= tolerance {
		result.Status = "unchanged"
		return result, nil
	}

	currentVol := volume
	currentMeter := meterBefore
	capped := false
	for i := 0; i < maxIters; i++ {
		nextVol, hitCap := nextAutogainVolume(currentVol, currentMeter, target, autogainMaxVolumeStep)
		capped = capped || hitCap
		if math.Abs(nextVol-currentVol) < 1e-4 {
			break
		}
		if err := client.Send("/live/track/set/volume", int32(trackIndex), float32(nextVol)); err != nil {
			return AutogainTrackResult{}, fmt.Errorf("set volume: %w", err)
		}
		currentVol = nextVol
		result.Iterations++
		if settleMs > 0 {
			sleep(time.Duration(settleMs) * time.Millisecond)
		}
		currentMeter, err = sampleTrackMeter(client, trackIndex, defaultAutogainMeterSamples)
		if err != nil {
			return AutogainTrackResult{}, err
		}
		if currentMeter < autogainSilentThreshold {
			break
		}
		if math.Abs(currentMeter-target) <= tolerance {
			break
		}
	}

	result.VolumeAfter = currentVol
	result.MeterAfter = currentMeter
	switch {
	case math.Abs(currentMeter-target) <= tolerance:
		result.Status = "ok"
	case capped:
		result.Status = "capped"
	default:
		result.Status = "partial"
	}
	return result, nil
}

func queryTrackVolume(client autogainClient, trackIndex int) (float64, error) {
	res, err := client.Query("/live/track/get/volume", int32(trackIndex))
	if err != nil {
		return 0, fmt.Errorf("get volume: %w", err)
	}
	if err := ensureResponseLen(res, 2); err != nil {
		return 0, fmt.Errorf("get volume: %w", err)
	}
	return abletonosc.AsFloat64(res[1])
}

func sampleTrackMeter(client autogainClient, trackIndex, samples int) (float64, error) {
	if samples < 1 {
		samples = 1
	}
	peak := 0.0
	for i := 0; i < samples; i++ {
		level, err := queryMeterInterface(client, "/live/track/get/output_meter_level", int32(trackIndex))
		if err != nil {
			return 0, fmt.Errorf("get meter: %w", err)
		}
		if level > peak {
			peak = level
		}
	}
	return peak, nil
}

func queryMeterInterface(client autogainClient, address string, args ...interface{}) (float64, error) {
	res, err := client.Query(address, args...)
	if err != nil {
		return 0, err
	}
	if len(res) >= 2 {
		return abletonosc.AsFloat64(res[1])
	}
	if len(res) >= 1 {
		return abletonosc.AsFloat64(res[0])
	}
	return 0, errors.New("empty meter response")
}

// nextAutogainVolume scales volume toward the target meter reading, limiting one-step change.
// Returns the next volume and whether the step limit was hit.
func nextAutogainVolume(currentVol, meterLevel, target, maxStep float64) (float64, bool) {
	if meterLevel <= 0 || currentVol <= 0 {
		return currentVol, false
	}
	desired := currentVol * (target / meterLevel)
	delta := desired - currentVol
	hitCap := false
	if delta > maxStep {
		delta = maxStep
		hitCap = true
	} else if delta < -maxStep {
		delta = -maxStep
		hitCap = true
	}
	next := currentVol + delta
	if next < 0 {
		next = 0
		hitCap = true
	}
	if next > 1 {
		next = 1
		hitCap = true
	}
	return next, hitCap
}
