package tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type CreateClipInput struct {
	TrackIndex  int     `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex   int     `json:"clip_index" jsonschema:"minimum=0"`
	LengthBeats float64 `json:"length_beats" jsonschema:"description=Clip length in beats (4 beats = 1 bar in 4/4),minimum=0.25"`
}

type HasClipOutput struct {
	HasClip bool `json:"has_clip"`
}

type FireClipSlotInput struct {
	TrackIndex int `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"minimum=0"`
}

type FiredOutput struct {
	Fired bool `json:"fired"`
}

type ClearClipNotesInput struct {
	TrackIndex int `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"minimum=0"`
}

type ClearedOutput struct {
	Cleared bool `json:"cleared"`
}

type AddMidiNotesInput struct {
	TrackIndex int        `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int        `json:"clip_index" jsonschema:"minimum=0"`
	Notes      []MidiNote `json:"notes,omitempty" jsonschema:"description=Notes to add as array"`
	NotesJson  string     `json:"notes_json,omitempty" jsonschema:"description=Notes as JSON string (alternative to notes array). Format: [{pitch:60,start_time:0,duration:0.5,velocity:100}]"`
}

type AddedOutput struct {
	Added int `json:"added"`
}

type ClipNotesInput struct {
	TrackIndex int `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"minimum=0"`

	StartPitch *int     `json:"start_pitch,omitempty" jsonschema:"minimum=0,maximum=127"`
	PitchSpan  *int     `json:"pitch_span,omitempty" jsonschema:"minimum=1,maximum=128"`
	StartTime  *float64 `json:"start_time,omitempty" jsonschema:"description=Start time in beats (float)"`
	TimeSpan   *float64 `json:"time_span,omitempty" jsonschema:"description=Time span in beats (float)"`
}

type ClipNotesOutput struct {
	TrackIndex int        `json:"track_index"`
	ClipIndex  int        `json:"clip_index"`
	Notes      []MidiNote `json:"notes"`
}

func NewAbletonCreateClip(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_clip", "Ableton Live: create clip",
		func(_ *ai.ToolContext, input CreateClipInput) (HasClipOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return HasClipOutput{}, err
			}
			if input.LengthBeats <= 0 {
				return HasClipOutput{}, errors.New("length_beats must be > 0")
			}
			if err := client.Send("/live/clip_slot/create_clip",
				int32(input.TrackIndex),
				int32(input.ClipIndex),
				float32(input.LengthBeats),
			); err != nil {
				return HasClipOutput{}, err
			}
			res, err := client.Query("/live/clip_slot/get/has_clip", int32(input.TrackIndex), int32(input.ClipIndex))
			if err != nil {
				return HasClipOutput{}, err
			}
			if err := ensureResponseLen(res, 3); err != nil {
				return HasClipOutput{}, err
			}
			has, err := abletonosc.AsBool(res[2])
			if err != nil {
				return HasClipOutput{}, err
			}
			return HasClipOutput{HasClip: has}, nil
		},
	)
}

func NewAbletonGetClipNotes(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_clip_notes", "Ableton Live: get MIDI notes in a clip",
		func(_ *ai.ToolContext, input ClipNotesInput) (ClipNotesOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return ClipNotesOutput{}, err
			}
			rangeProvided := input.StartPitch != nil || input.PitchSpan != nil || input.StartTime != nil || input.TimeSpan != nil
			if rangeProvided {
				if input.StartPitch == nil || input.PitchSpan == nil || input.StartTime == nil || input.TimeSpan == nil {
					return ClipNotesOutput{}, errors.New("start_pitch, pitch_span, start_time, time_span must be set together")
				}
				if *input.StartPitch < 0 || *input.StartPitch > 127 {
					return ClipNotesOutput{}, errors.New("start_pitch must be 0..127")
				}
				if *input.PitchSpan <= 0 || *input.PitchSpan > 128 {
					return ClipNotesOutput{}, errors.New("pitch_span must be 1..128")
				}
				if *input.TimeSpan <= 0 {
					return ClipNotesOutput{}, errors.New("time_span must be > 0")
				}
			}

			args := []interface{}{int32(input.TrackIndex), int32(input.ClipIndex)}
			if rangeProvided {
				args = append(args,
					int32(*input.StartPitch),
					int32(*input.PitchSpan),
					float32(*input.StartTime),
					float32(*input.TimeSpan),
				)
			}

			res, err := client.Query("/live/clip/get/notes", args...)
			if err != nil {
				return ClipNotesOutput{}, err
			}
			if err := ensureResponseLen(res, 2); err != nil {
				return ClipNotesOutput{}, err
			}
			trackIndex, err := abletonosc.AsInt(res[0])
			if err != nil {
				return ClipNotesOutput{}, err
			}
			clipIndex, err := abletonosc.AsInt(res[1])
			if err != nil {
				return ClipNotesOutput{}, err
			}

			payload := res[2:]
			if len(payload)%5 != 0 {
				return ClipNotesOutput{}, fmt.Errorf("unexpected notes payload: %v", payload)
			}

			notes := make([]MidiNote, 0, len(payload)/5)
			for i := 0; i < len(payload); i += 5 {
				pitch, err := abletonosc.AsInt(payload[i])
				if err != nil {
					return ClipNotesOutput{}, err
				}
				startTime, err := abletonosc.AsFloat64(payload[i+1])
				if err != nil {
					return ClipNotesOutput{}, err
				}
				duration, err := abletonosc.AsFloat64(payload[i+2])
				if err != nil {
					return ClipNotesOutput{}, err
				}
				velocity, err := abletonosc.AsInt(payload[i+3])
				if err != nil {
					return ClipNotesOutput{}, err
				}
				mute, err := abletonosc.AsBool(payload[i+4])
				if err != nil {
					return ClipNotesOutput{}, err
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

			return ClipNotesOutput{
				TrackIndex: trackIndex,
				ClipIndex:  clipIndex,
				Notes:      notes,
			}, nil
		},
	)
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

func NewAbletonClearClipNotes(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_clear_clip_notes", "Ableton Live: clear all notes in a clip",
		func(_ *ai.ToolContext, input ClearClipNotesInput) (ClearedOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return ClearedOutput{}, err
			}
			// AbletonOSC: Passing only (track_index, clip_index) clears all notes.
			if err := client.Send("/live/clip/remove/notes", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
				return ClearedOutput{}, err
			}
			return ClearedOutput{Cleared: true}, nil
		},
	)
}

func NewAbletonAddMidiNotes(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_add_midi_notes", "Ableton Live: add MIDI notes to a clip",
		func(_ *ai.ToolContext, input AddMidiNotesInput) (AddedOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return AddedOutput{}, err
			}

			// Parse notes from JSON string if notes array is empty but notes_json is provided
			notes := input.Notes
			if len(notes) == 0 && input.NotesJson != "" {
				if err := json.Unmarshal([]byte(input.NotesJson), &notes); err != nil {
					return AddedOutput{}, fmt.Errorf("failed to parse notes_json: %w", err)
				}
			}

			if len(notes) == 0 {
				return AddedOutput{}, errors.New("notes must not be empty (provide either 'notes' array or 'notes_json' string)")
			}
			args := []interface{}{int32(input.TrackIndex), int32(input.ClipIndex)}
			for _, n := range notes {
				if n.Pitch < 0 || n.Pitch > 127 {
					return AddedOutput{}, errors.New("pitch must be 0..127")
				}
				if n.Duration <= 0 {
					return AddedOutput{}, errors.New("duration must be > 0")
				}
				if n.StartTime < 0 {
					return AddedOutput{}, errors.New("start_time must be >= 0")
				}
				if n.Velocity < 1 || n.Velocity > 127 {
					return AddedOutput{}, errors.New("velocity must be 1..127")
				}
				mute := false
				if n.Mute != nil {
					mute = *n.Mute
				}
				args = append(args,
					int32(n.Pitch),
					float32(n.StartTime),
					float32(n.Duration),
					int32(n.Velocity),
					mute,
				)
			}
			if err := client.Send("/live/clip/add/notes", args...); err != nil {
				return AddedOutput{}, err
			}
			return AddedOutput{Added: len(notes)}, nil
		},
	)
}
