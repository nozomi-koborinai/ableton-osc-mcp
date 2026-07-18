package tools

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	defaultAuditionBarsPerVersion = 2
	defaultAuditionCycles         = 1
	defaultAuditionBeatsPerBar    = 4
)

type AuditionABInput struct {
	TargetType     string `json:"target_type" jsonschema:"description=What to audition: clip or scene"`
	TrackIndex     *int   `json:"track_index,omitempty" jsonschema:"description=Required for clip auditions; omit for scenes,minimum=0"`
	SourceIndex    int    `json:"source_index" jsonschema:"description=A version: clip slot or scene index,minimum=0"`
	VariationIndex int    `json:"variation_index" jsonschema:"description=B version: clip slot or scene index,minimum=0"`
	BarsPerVersion *int   `json:"bars_per_version,omitempty" jsonschema:"description=Bars to hear each version (default 2),minimum=1,maximum=8"`
	Cycles         *int   `json:"cycles,omitempty" jsonschema:"description=How many A→B cycles to play (default 1),minimum=1,maximum=4"`
	BeatsPerBar    *int   `json:"beats_per_bar,omitempty" jsonschema:"description=Beats per bar for duration calculation (default 4),minimum=1,maximum=16"`
	StartPlayback  bool   `json:"start_playback,omitempty" jsonschema:"description=Start Live playback before the audition"`
	StopAfter      bool   `json:"stop_after,omitempty" jsonschema:"description=Stop playback after the final B version"`
}

type AuditionABOutput struct {
	TargetType       string  `json:"target_type"`
	TrackIndex       *int    `json:"track_index,omitempty"`
	SourceIndex      int     `json:"source_index"`
	VariationIndex   int     `json:"variation_index"`
	BarsPerVersion   int     `json:"bars_per_version"`
	Cycles           int     `json:"cycles"`
	TempoBPM         float64 `json:"tempo_bpm"`
	DurationSec      float64 `json:"duration_sec"`
	FinalVersion     string  `json:"final_version"`
	TimingNote       string  `json:"timing_note"`
	PreferencePrompt string  `json:"preference_prompt"`
}

type auditionClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

type auditionSleeper func(time.Duration)

func NewAbletonAuditionAB(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_audition_ab",
		"Ableton Live: audition an A and B clip or scene in alternating bar-length sections, then prompt for a preference",
		func(_ *ai.ToolContext, input AuditionABInput) (AuditionABOutput, error) {
			return auditionAB(client, input, time.Sleep)
		},
	)
}

func auditionAB(client auditionClient, input AuditionABInput, sleep auditionSleeper) (AuditionABOutput, error) {
	targetType, bars, cycles, beatsPerBar, err := validateAuditionInput(input)
	if err != nil {
		return AuditionABOutput{}, err
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	tempoRes, err := client.Query("/live/song/get/tempo")
	if err != nil {
		return AuditionABOutput{}, fmt.Errorf("get tempo: %w", err)
	}
	if err := ensureResponseLen(tempoRes, 1); err != nil {
		return AuditionABOutput{}, fmt.Errorf("get tempo: %w", err)
	}
	tempo, err := abletonosc.AsFloat64(tempoRes[0])
	if err != nil || tempo <= 0 {
		return AuditionABOutput{}, fmt.Errorf("unexpected tempo: %v", tempoRes)
	}
	wait := auditionWaitDuration(tempo, bars, beatsPerBar)

	if input.StartPlayback {
		if err := client.Send("/live/song/start_playing"); err != nil {
			return AuditionABOutput{}, fmt.Errorf("start playback: %w", err)
		}
	}

	for i := 0; i < cycles; i++ {
		if err := fireAuditionTarget(client, targetType, input.TrackIndex, input.SourceIndex); err != nil {
			return AuditionABOutput{}, fmt.Errorf("fire A (cycle %d): %w", i+1, err)
		}
		sleep(wait)
		if err := fireAuditionTarget(client, targetType, input.TrackIndex, input.VariationIndex); err != nil {
			return AuditionABOutput{}, fmt.Errorf("fire B (cycle %d): %w", i+1, err)
		}
		sleep(wait)
	}
	if input.StopAfter {
		if err := client.Send("/live/song/stop_playing"); err != nil {
			return AuditionABOutput{}, fmt.Errorf("stop playback: %w", err)
		}
	}

	var trackIndex *int
	if input.TrackIndex != nil {
		index := *input.TrackIndex
		trackIndex = &index
	}
	return AuditionABOutput{
		TargetType:       targetType,
		TrackIndex:       trackIndex,
		SourceIndex:      input.SourceIndex,
		VariationIndex:   input.VariationIndex,
		BarsPerVersion:   bars,
		Cycles:           cycles,
		TempoBPM:         tempo,
		DurationSec:      wait.Seconds() * float64(cycles*2),
		FinalVersion:     "variation",
		TimingNote:       "Uses Live's current global quantization; duration is a tempo-based estimate.",
		PreferencePrompt: "Which was closer to your ideal: source or variation? Record the choice with ableton_record_variation_preference.",
	}, nil
}

func validateAuditionInput(input AuditionABInput) (string, int, int, int, error) {
	targetType := strings.ToLower(strings.TrimSpace(input.TargetType))
	if targetType != "clip" && targetType != "scene" {
		return "", 0, 0, 0, errors.New("target_type must be clip or scene")
	}
	if input.SourceIndex < 0 || input.VariationIndex < 0 {
		return "", 0, 0, 0, errors.New("source_index and variation_index must be >= 0")
	}
	if input.SourceIndex == input.VariationIndex {
		return "", 0, 0, 0, errors.New("source_index and variation_index must differ")
	}
	if targetType == "clip" {
		if input.TrackIndex == nil {
			return "", 0, 0, 0, errors.New("track_index is required for clip auditions")
		}
		if *input.TrackIndex < 0 {
			return "", 0, 0, 0, errors.New("track_index must be >= 0")
		}
	} else if input.TrackIndex != nil {
		return "", 0, 0, 0, errors.New("track_index must be omitted for scene auditions")
	}

	bars := defaultAuditionBarsPerVersion
	if input.BarsPerVersion != nil {
		bars = *input.BarsPerVersion
	}
	cycles := defaultAuditionCycles
	if input.Cycles != nil {
		cycles = *input.Cycles
	}
	beatsPerBar := defaultAuditionBeatsPerBar
	if input.BeatsPerBar != nil {
		beatsPerBar = *input.BeatsPerBar
	}
	if bars < 1 || bars > 8 {
		return "", 0, 0, 0, errors.New("bars_per_version must be between 1 and 8")
	}
	if cycles < 1 || cycles > 4 {
		return "", 0, 0, 0, errors.New("cycles must be between 1 and 4")
	}
	if beatsPerBar < 1 || beatsPerBar > 16 {
		return "", 0, 0, 0, errors.New("beats_per_bar must be between 1 and 16")
	}
	return targetType, bars, cycles, beatsPerBar, nil
}

func fireAuditionTarget(client auditionClient, targetType string, trackIndex *int, index int) error {
	if targetType == "scene" {
		return client.Send("/live/scene/fire", int32(index))
	}
	return client.Send("/live/clip_slot/fire", int32(*trackIndex), int32(index))
}

func auditionWaitDuration(tempo float64, bars, beatsPerBar int) time.Duration {
	seconds := float64(bars*beatsPerBar) * 60 / tempo
	return time.Duration(seconds * float64(time.Second))
}
