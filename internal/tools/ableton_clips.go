package tools

import (
	"errors"
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type StopClipInput struct {
	TrackIndex int `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"description=Clip slot index (0-based; same row as the scene),minimum=0"`
}

type DuplicateClipToInput struct {
	TrackIndex       int  `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	ClipIndex        int  `json:"clip_index" jsonschema:"description=Clip slot index (0-based; same row as the scene),minimum=0"`
	TargetClipIndex  int  `json:"target_clip_index" jsonschema:"description=Target clip slot (scene) index to duplicate to,minimum=0"`
	TargetTrackIndex *int `json:"target_track_index,omitempty" jsonschema:"description=Target track index; omit to duplicate within the same track,minimum=0"`
}

type HasClipOutput struct {
	HasClip bool `json:"has_clip"`
}

type FireClipSlotInput struct {
	TrackIndex int `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"description=Clip slot index (0-based; same row as the scene),minimum=0"`
}

type FiredOutput struct {
	Fired bool `json:"fired"`
}

func parseClipNotesResponse(res []interface{}) (int, int, []MidiNote, error) {
	if err := ensureResponseLen(res, 2); err != nil {
		return 0, 0, nil, err
	}
	trackIndex, err := abletonosc.AsInt(res[0])
	if err != nil {
		return 0, 0, nil, err
	}
	clipIndex, err := abletonosc.AsInt(res[1])
	if err != nil {
		return 0, 0, nil, err
	}

	payload := res[2:]
	if len(payload)%5 != 0 {
		return 0, 0, nil, fmt.Errorf("unexpected notes payload: %v", payload)
	}

	notes := make([]MidiNote, 0, len(payload)/5)
	for i := 0; i < len(payload); i += 5 {
		pitch, err := abletonosc.AsInt(payload[i])
		if err != nil {
			return 0, 0, nil, err
		}
		startTime, err := abletonosc.AsFloat64(payload[i+1])
		if err != nil {
			return 0, 0, nil, err
		}
		duration, err := abletonosc.AsFloat64(payload[i+2])
		if err != nil {
			return 0, 0, nil, err
		}
		velocity, err := abletonosc.AsInt(payload[i+3])
		if err != nil {
			return 0, 0, nil, err
		}
		mute, err := abletonosc.AsBool(payload[i+4])
		if err != nil {
			return 0, 0, nil, err
		}
		m := mute
		notes = append(notes, MidiNote{
			Pitch:     pitch,
			StartTime: startTime,
			Duration:  duration,
			Velocity:  velocity,
			Mute:      &m,
		})
	}
	return trackIndex, clipIndex, notes, nil
}

func NewAbletonFireClipSlot(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_fire_clip_slot", "Ableton Live: fire clip slot",
		func(_ *ai.ToolContext, input FireClipSlotInput) (FiredOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return FiredOutput{}, err
			}
			if err := client.Send("/live/clip_slot/fire", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
				return FiredOutput{}, err
			}
			return FiredOutput{Fired: true}, nil
		},
	)
}

func NewAbletonStopClip(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_stop_clip", "Ableton Live: stop a clip",
		func(_ *ai.ToolContext, input StopClipInput) (SentOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return SentOutput{}, err
			}
			if err := client.Send("/live/clip/stop", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonDuplicateClipTo(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_duplicate_clip_to",
		"Ableton Live: duplicate a clip to another slot (same track by default, or cross-track when target_track_index is set)",
		func(_ *ai.ToolContext, input DuplicateClipToInput) (SentOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return SentOutput{}, err
			}
			if input.TargetClipIndex < 0 {
				return SentOutput{}, errors.New("target_clip_index must be >= 0")
			}
			targetTrack := input.TrackIndex
			if input.TargetTrackIndex != nil {
				if *input.TargetTrackIndex < 0 {
					return SentOutput{}, errors.New("target_track_index must be >= 0")
				}
				targetTrack = *input.TargetTrackIndex
			}
			if err := client.Send("/live/clip_slot/duplicate_clip_to",
				int32(input.TrackIndex),
				int32(input.ClipIndex),
				int32(targetTrack),
				int32(input.TargetClipIndex),
			); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}
