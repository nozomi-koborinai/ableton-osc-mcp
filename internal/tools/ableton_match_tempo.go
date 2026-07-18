package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	warpModeBeats   = 0
	warpModeComplex = 4
)

type MatchClipTempoInput struct {
	TrackIndex int    `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int    `json:"clip_index" jsonschema:"minimum=0"`
	WarpMode   string `json:"warp_mode,omitempty" jsonschema:"description=Warp algorithm: beats (default, good for drums/loops) or complex (longer tonal samples)"`
	Fire       bool   `json:"fire,omitempty" jsonschema:"description=Fire the clip after enabling warp"`
}

type MatchClipTempoOutput struct {
	TrackIndex        int     `json:"track_index"`
	ClipIndex         int     `json:"clip_index"`
	TempoBPM          float64 `json:"tempo_bpm"`
	Warping           bool    `json:"warping"`
	WarpMode          string  `json:"warp_mode"`
	LengthBeats       float64 `json:"length_beats"`
	LengthBeatsBefore float64 `json:"length_beats_before"`
	Fired             bool    `json:"fired"`
	Note              string  `json:"note"`
}

type matchTempoClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonMatchClipTempo(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_match_clip_tempo",
		"Ableton Live: enable Warp on an audio clip so it follows the project tempo (useful after loading a Splice sample)",
		func(_ *ai.ToolContext, input MatchClipTempoInput) (MatchClipTempoOutput, error) {
			return matchClipTempo(client, input)
		},
	)
}

func matchClipTempo(client matchTempoClient, input MatchClipTempoInput) (MatchClipTempoOutput, error) {
	if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
		return MatchClipTempoOutput{}, err
	}
	warpModeName, warpModeValue, err := resolveWarpMode(input.WarpMode)
	if err != nil {
		return MatchClipTempoOutput{}, err
	}

	hasClip, err := queryMatchTempoHasClip(client, input.TrackIndex, input.ClipIndex)
	if err != nil {
		return MatchClipTempoOutput{}, fmt.Errorf("check clip slot: %w", err)
	}
	if !hasClip {
		return MatchClipTempoOutput{}, errors.New("clip slot is empty")
	}

	isAudio, err := queryClipIsAudio(client, input.TrackIndex, input.ClipIndex)
	if err != nil {
		return MatchClipTempoOutput{}, err
	}
	if !isAudio {
		return MatchClipTempoOutput{}, errors.New("match tempo only works on audio clips")
	}

	tempo, err := queryAuditionTempo(client)
	if err != nil {
		return MatchClipTempoOutput{}, err
	}

	lengthBefore := queryMatchTempoClipLength(client, input.TrackIndex, input.ClipIndex)

	if err := client.Send("/live/clip/set/warping", int32(input.TrackIndex), int32(input.ClipIndex), int32(1)); err != nil {
		return MatchClipTempoOutput{}, fmt.Errorf("enable warping: %w", err)
	}
	if err := client.Send("/live/clip/set/warp_mode", int32(input.TrackIndex), int32(input.ClipIndex), int32(warpModeValue)); err != nil {
		return MatchClipTempoOutput{}, fmt.Errorf("set warp mode: %w", err)
	}

	warping, err := queryClipWarping(client, input.TrackIndex, input.ClipIndex)
	if err != nil {
		return MatchClipTempoOutput{}, err
	}
	lengthAfter := queryMatchTempoClipLength(client, input.TrackIndex, input.ClipIndex)

	fired := false
	if input.Fire {
		if err := client.Send("/live/clip_slot/fire", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
			return MatchClipTempoOutput{}, fmt.Errorf("fire clip: %w", err)
		}
		fired = true
	}

	return MatchClipTempoOutput{
		TrackIndex:        input.TrackIndex,
		ClipIndex:         input.ClipIndex,
		TempoBPM:          tempo,
		Warping:           warping,
		WarpMode:          warpModeName,
		LengthBeats:       lengthAfter,
		LengthBeatsBefore: lengthBefore,
		Fired:             fired,
		Note:              "Warp is on so the clip follows the project tempo. Adjust warp markers in Live if the transient alignment is off.",
	}, nil
}

func resolveWarpMode(raw string) (string, int, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		mode = "beats"
	}
	switch mode {
	case "beats":
		return "beats", warpModeBeats, nil
	case "complex":
		return "complex", warpModeComplex, nil
	default:
		return "", 0, errors.New("warp_mode must be beats or complex")
	}
}

func queryMatchTempoHasClip(client matchTempoClient, trackIndex, clipIndex int) (bool, error) {
	res, err := client.Query("/live/clip_slot/get/has_clip", int32(trackIndex), int32(clipIndex))
	if err != nil {
		return false, err
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, err
	}
	return abletonosc.AsBool(res[2])
}

func queryClipIsAudio(client matchTempoClient, trackIndex, clipIndex int) (bool, error) {
	res, err := client.Query("/live/clip/get/is_audio_clip", int32(trackIndex), int32(clipIndex))
	if err != nil {
		return false, fmt.Errorf("get is_audio_clip: %w", err)
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, fmt.Errorf("get is_audio_clip: %w", err)
	}
	isAudio, err := abletonosc.AsBool(res[2])
	if err != nil {
		return false, fmt.Errorf("get is_audio_clip: %w", err)
	}
	return isAudio, nil
}

func queryClipWarping(client matchTempoClient, trackIndex, clipIndex int) (bool, error) {
	res, err := client.Query("/live/clip/get/warping", int32(trackIndex), int32(clipIndex))
	if err != nil {
		return false, fmt.Errorf("get warping: %w", err)
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, fmt.Errorf("get warping: %w", err)
	}
	warping, err := abletonosc.AsBool(res[2])
	if err != nil {
		return false, fmt.Errorf("get warping: %w", err)
	}
	return warping, nil
}

func queryMatchTempoClipLength(client matchTempoClient, trackIndex, clipIndex int) float64 {
	res, err := client.Query("/live/clip/get/length", int32(trackIndex), int32(clipIndex))
	if err != nil || len(res) == 0 {
		return 0
	}
	value := res[len(res)-1]
	length, err := abletonosc.AsFloat64(value)
	if err != nil || length < 0 {
		return 0
	}
	return length
}
