package tools

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	defaultAuditionBarsPerVersion = 2
	defaultAuditionCycles         = 1
	// Live clip_trigger_quantization: 4 = 1 Bar (see Live Object Model).
	auditionBarQuantization = 4
	auditionSongTimeEpsilon = 1e-3
	auditionPollInterval    = 20 * time.Millisecond
)

type AuditionABInput struct {
	TargetType     string `json:"target_type" jsonschema:"description=What to audition: clip or scene"`
	TrackIndex     *int   `json:"track_index,omitempty" jsonschema:"description=Required for clip auditions; omit for scenes,minimum=0"`
	SourceIndex    int    `json:"source_index" jsonschema:"description=A version: clip slot or scene index,minimum=0"`
	VariationIndex int    `json:"variation_index" jsonschema:"description=B version: clip slot or scene index,minimum=0"`
	BarsPerVersion *int   `json:"bars_per_version,omitempty" jsonschema:"description=Bars to hear each version (default 2),minimum=1,maximum=8"`
	Cycles         *int   `json:"cycles,omitempty" jsonschema:"description=How many A→B cycles to play (default 1),minimum=1,maximum=4"`
	BeatsPerBar    *int   `json:"beats_per_bar,omitempty" jsonschema:"description=Override beats per bar (default: Live signature numerator),minimum=1,maximum=16"`
	Instrument     string `json:"instrument,omitempty" jsonschema:"description=Optional taste family for the preference prompt: drum or bass for clips; scene for scenes"`
	Variation      string `json:"variation,omitempty" jsonschema:"description=Optional variation that was compared (e.g. groove, lift) for the preference prompt"`
	StartPlayback  bool   `json:"start_playback,omitempty" jsonschema:"description=Start Live playback before the audition (also auto-starts when transport is stopped)"`
	StopAfter      bool   `json:"stop_after,omitempty" jsonschema:"description=Stop playback after the final B version"`
}

type AuditionABOutput struct {
	TargetType       string  `json:"target_type"`
	TrackIndex       *int    `json:"track_index,omitempty"`
	SourceIndex      int     `json:"source_index"`
	VariationIndex   int     `json:"variation_index"`
	BarsPerVersion   int     `json:"bars_per_version"`
	Cycles           int     `json:"cycles"`
	BeatsPerBar      int     `json:"beats_per_bar"`
	TempoBPM         float64 `json:"tempo_bpm"`
	DurationSec      float64 `json:"duration_sec"`
	PlaybackStarted  bool    `json:"playback_started"`
	Instrument       string  `json:"instrument,omitempty"`
	Variation        string  `json:"variation,omitempty"`
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
		"Ableton Live: audition an A and B clip or scene in alternating bar-length sections synced to song time, then prompt for a preference",
		func(_ *ai.ToolContext, input AuditionABInput) (AuditionABOutput, error) {
			return auditionAB(client, input, time.Sleep)
		},
	)
}

func auditionAB(client auditionClient, input AuditionABInput, sleep auditionSleeper) (AuditionABOutput, error) {
	targetType, bars, cycles, beatsPerBarOverride, instrument, variation, err := validateAuditionInput(input)
	if err != nil {
		return AuditionABOutput{}, err
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	tempo, err := queryAuditionTempo(client)
	if err != nil {
		return AuditionABOutput{}, err
	}
	beatsPerBar := beatsPerBarOverride
	if beatsPerBar == 0 {
		beatsPerBar, err = queryAuditionBeatsPerBar(client)
		if err != nil {
			return AuditionABOutput{}, err
		}
	}

	playbackStarted, err := ensureAuditionPlayback(client, input.StartPlayback)
	if err != nil {
		return AuditionABOutput{}, err
	}

	prevQuant, err := queryClipTriggerQuantization(client)
	if err != nil {
		return AuditionABOutput{}, err
	}
	if err := client.Send("/live/song/set/clip_trigger_quantization", int32(auditionBarQuantization)); err != nil {
		return AuditionABOutput{}, fmt.Errorf("set clip trigger quantization: %w", err)
	}
	restoreQuant := true
	defer func() {
		if restoreQuant {
			_ = client.Send("/live/song/set/clip_trigger_quantization", int32(prevQuant))
		}
	}()

	heardBeats := 0.0
	for i := 0; i < cycles; i++ {
		heard, err := fireAndHearAudition(client, sleep, targetType, input.TrackIndex, input.SourceIndex, bars, beatsPerBar, tempo)
		if err != nil {
			return AuditionABOutput{}, fmt.Errorf("fire A (cycle %d): %w", i+1, err)
		}
		heardBeats += heard
		heard, err = fireAndHearAudition(client, sleep, targetType, input.TrackIndex, input.VariationIndex, bars, beatsPerBar, tempo)
		if err != nil {
			return AuditionABOutput{}, fmt.Errorf("fire B (cycle %d): %w", i+1, err)
		}
		heardBeats += heard
	}

	if input.StopAfter {
		if err := client.Send("/live/song/stop_playing"); err != nil {
			return AuditionABOutput{}, fmt.Errorf("stop playback: %w", err)
		}
	}

	if err := client.Send("/live/song/set/clip_trigger_quantization", int32(prevQuant)); err != nil {
		return AuditionABOutput{}, fmt.Errorf("restore clip trigger quantization: %w", err)
	}
	restoreQuant = false

	var trackIndex *int
	if input.TrackIndex != nil {
		index := *input.TrackIndex
		trackIndex = &index
	}
	durationSec := heardBeats * 60 / tempo
	return AuditionABOutput{
		TargetType:       targetType,
		TrackIndex:       trackIndex,
		SourceIndex:      input.SourceIndex,
		VariationIndex:   input.VariationIndex,
		BarsPerVersion:   bars,
		Cycles:           cycles,
		BeatsPerBar:      beatsPerBar,
		TempoBPM:         tempo,
		DurationSec:      durationSec,
		PlaybackStarted:  playbackStarted,
		Instrument:       instrument,
		Variation:        variation,
		FinalVersion:     "variation",
		TimingNote:       "Waits on Live song time with 1-bar clip trigger quantization (restored afterward). Switches land on the next bar boundary.",
		PreferencePrompt: auditionPreferencePrompt(targetType, instrument, variation),
	}, nil
}

func fireAndHearAudition(
	client auditionClient,
	sleep auditionSleeper,
	targetType string,
	trackIndex *int,
	index, bars, beatsPerBar int,
	tempo float64,
) (float64, error) {
	now, err := queryCurrentSongTime(client)
	if err != nil {
		return 0, err
	}
	if err := fireAuditionTarget(client, targetType, trackIndex, index); err != nil {
		return 0, err
	}
	launchBeat := ceilBarBeat(now, beatsPerBar)
	endBeat := launchBeat + float64(bars*beatsPerBar)
	if err := waitUntilSongTime(client, sleep, endBeat, tempo); err != nil {
		return 0, err
	}
	return endBeat - now, nil
}

func ensureAuditionPlayback(client auditionClient, forceStart bool) (bool, error) {
	playing, err := queryAuditionIsPlaying(client)
	if err != nil {
		return false, err
	}
	if playing && !forceStart {
		return false, nil
	}
	if err := client.Send("/live/song/start_playing"); err != nil {
		return false, fmt.Errorf("start playback: %w", err)
	}
	return !playing, nil
}

func queryAuditionTempo(client auditionClient) (float64, error) {
	tempoRes, err := client.Query("/live/song/get/tempo")
	if err != nil {
		return 0, fmt.Errorf("get tempo: %w", err)
	}
	if err := ensureResponseLen(tempoRes, 1); err != nil {
		return 0, fmt.Errorf("get tempo: %w", err)
	}
	tempo, err := abletonosc.AsFloat64(tempoRes[0])
	if err != nil || tempo <= 0 {
		return 0, fmt.Errorf("unexpected tempo: %v", tempoRes)
	}
	return tempo, nil
}

func queryAuditionBeatsPerBar(client auditionClient) (int, error) {
	res, err := client.Query("/live/song/get/signature_numerator")
	if err != nil {
		return 0, fmt.Errorf("get signature numerator: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, fmt.Errorf("get signature numerator: %w", err)
	}
	beats, err := abletonosc.AsInt(res[0])
	if err != nil || beats < 1 || beats > 16 {
		return 0, fmt.Errorf("unexpected signature numerator: %v", res)
	}
	return beats, nil
}

func queryAuditionIsPlaying(client auditionClient) (bool, error) {
	res, err := client.Query("/live/song/get/is_playing")
	if err != nil {
		return false, fmt.Errorf("get playback state: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return false, fmt.Errorf("get playback state: %w", err)
	}
	playing, err := abletonosc.AsBool(res[0])
	if err != nil {
		return false, fmt.Errorf("get playback state: %w", err)
	}
	return playing, nil
}

func queryClipTriggerQuantization(client auditionClient) (int, error) {
	res, err := client.Query("/live/song/get/clip_trigger_quantization")
	if err != nil {
		return 0, fmt.Errorf("get clip trigger quantization: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, fmt.Errorf("get clip trigger quantization: %w", err)
	}
	quant, err := abletonosc.AsInt(res[0])
	if err != nil {
		return 0, fmt.Errorf("get clip trigger quantization: %w", err)
	}
	return quant, nil
}

func queryCurrentSongTime(client auditionClient) (float64, error) {
	res, err := client.Query("/live/song/get/current_song_time")
	if err != nil {
		return 0, fmt.Errorf("get current song time: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, fmt.Errorf("get current song time: %w", err)
	}
	songTime, err := abletonosc.AsFloat64(res[0])
	if err != nil {
		return 0, fmt.Errorf("get current song time: %w", err)
	}
	return songTime, nil
}

func waitUntilSongTime(client auditionClient, sleep auditionSleeper, targetBeats, tempo float64) error {
	remainingBeats := targetBeats
	if now, err := queryCurrentSongTime(client); err == nil {
		remainingBeats = targetBeats - now
	}
	if remainingBeats < 0 {
		remainingBeats = 0
	}
	// Wall-clock deadline guards against a stuck transport; allow 2x expected wait + 2s.
	timeout := time.Duration((remainingBeats*60/tempo)*2*float64(time.Second)) + 2*time.Second
	deadline := time.Now().Add(timeout)

	for {
		now, err := queryCurrentSongTime(client)
		if err != nil {
			return err
		}
		if now+auditionSongTimeEpsilon >= targetBeats {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for song time %.3f (last %.3f)", targetBeats, now)
		}
		sleep(auditionPollInterval)
	}
}

// ceilBarBeat returns the next bar boundary strictly after songTime when quantized to 1 bar.
// Firing exactly on a downbeat still waits for the following bar under Live's 1-bar quantization.
func ceilBarBeat(songTime float64, beatsPerBar int) float64 {
	bpb := float64(beatsPerBar)
	if bpb <= 0 {
		return songTime
	}
	barStart := math.Floor(songTime/bpb+auditionSongTimeEpsilon) * bpb
	if songTime-barStart <= auditionSongTimeEpsilon {
		return barStart + bpb
	}
	return barStart + bpb
}

func validateAuditionInput(input AuditionABInput) (string, int, int, int, string, string, error) {
	targetType := strings.ToLower(strings.TrimSpace(input.TargetType))
	if targetType != "clip" && targetType != "scene" {
		return "", 0, 0, 0, "", "", errors.New("target_type must be clip or scene")
	}
	if input.SourceIndex < 0 || input.VariationIndex < 0 {
		return "", 0, 0, 0, "", "", errors.New("source_index and variation_index must be >= 0")
	}
	if input.SourceIndex == input.VariationIndex {
		return "", 0, 0, 0, "", "", errors.New("source_index and variation_index must differ")
	}
	if targetType == "clip" {
		if input.TrackIndex == nil {
			return "", 0, 0, 0, "", "", errors.New("track_index is required for clip auditions")
		}
		if *input.TrackIndex < 0 {
			return "", 0, 0, 0, "", "", errors.New("track_index must be >= 0")
		}
	} else if input.TrackIndex != nil {
		return "", 0, 0, 0, "", "", errors.New("track_index must be omitted for scene auditions")
	}

	bars := defaultAuditionBarsPerVersion
	if input.BarsPerVersion != nil {
		bars = *input.BarsPerVersion
	}
	cycles := defaultAuditionCycles
	if input.Cycles != nil {
		cycles = *input.Cycles
	}
	beatsPerBar := 0
	if input.BeatsPerBar != nil {
		beatsPerBar = *input.BeatsPerBar
		if beatsPerBar < 1 || beatsPerBar > 16 {
			return "", 0, 0, 0, "", "", errors.New("beats_per_bar must be between 1 and 16")
		}
	}
	if bars < 1 || bars > 8 {
		return "", 0, 0, 0, "", "", errors.New("bars_per_version must be between 1 and 8")
	}
	if cycles < 1 || cycles > 4 {
		return "", 0, 0, 0, "", "", errors.New("cycles must be between 1 and 4")
	}

	instrument, variation, err := validateAuditionTasteHint(targetType, input.Instrument, input.Variation)
	if err != nil {
		return "", 0, 0, 0, "", "", err
	}
	return targetType, bars, cycles, beatsPerBar, instrument, variation, nil
}

func validateAuditionTasteHint(targetType, instrument, variation string) (string, string, error) {
	instrument = strings.ToLower(strings.TrimSpace(instrument))
	variation = strings.ToLower(strings.TrimSpace(variation))
	if instrument == "" && variation == "" {
		return "", "", nil
	}
	if instrument == "" {
		if targetType == "scene" {
			instrument = "scene"
		} else {
			return "", "", errors.New("instrument is required when variation is set for clip auditions")
		}
	}
	switch targetType {
	case "clip":
		if instrument != "drum" && instrument != "bass" {
			return "", "", errors.New("clip audition instrument must be drum or bass")
		}
	case "scene":
		if instrument != "scene" {
			return "", "", errors.New("scene audition instrument must be scene")
		}
	}
	if variation != "" {
		if err := validateTasteInstrumentVariation(instrument, variation); err != nil {
			return "", "", err
		}
	}
	return instrument, variation, nil
}

func auditionPreferencePrompt(targetType, instrument, variation string) string {
	if instrument != "" && variation != "" {
		return fmt.Sprintf(
			"Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=%s variation=%s.",
			instrument, variation,
		)
	}
	if instrument != "" {
		candidates := strings.Join(tasteVariationsFor(instrument), ", ")
		return fmt.Sprintf(
			"Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=%s and the variation you compared (%s).",
			instrument, candidates,
		)
	}
	if targetType == "scene" {
		return "Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=scene and variation=lift or pullback."
	}
	return "Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=drum or bass and the variation you compared (e.g. groove, density, octave_up)."
}

func fireAuditionTarget(client auditionClient, targetType string, trackIndex *int, index int) error {
	if targetType == "scene" {
		return client.Send("/live/scene/fire", int32(index))
	}
	return client.Send("/live/clip_slot/fire", int32(*trackIndex), int32(index))
}
