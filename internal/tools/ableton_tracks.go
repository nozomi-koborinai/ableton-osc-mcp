package tools

import (
	"errors"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type TrackNamesInput struct {
	IndexMin *int `json:"index_min,omitempty" jsonschema:"description=Optional start index,minimum=0"`
	IndexMax *int `json:"index_max,omitempty" jsonschema:"description=Optional end index (exclusive),minimum=1"`
}

type TrackNamesOutput struct {
	TrackNames []string `json:"track_names"`
}

type CreateMidiTrackInput struct {
	Index *int `json:"index,omitempty" jsonschema:"description=Track index (-1 to append),minimum=-1"`
}

type NumTracksOutput struct {
	NumTracks int `json:"num_tracks"`
}

func NewAbletonGetTrackNames(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_track_names", "Ableton Live: list track names",
		func(_ *ai.ToolContext, input TrackNamesInput) (TrackNamesOutput, error) {
			var args []interface{}
			if (input.IndexMin == nil) != (input.IndexMax == nil) {
				return TrackNamesOutput{}, errors.New("index_min and index_max must be set together")
			}
			if input.IndexMin != nil && input.IndexMax != nil {
				if *input.IndexMin < 0 {
					return TrackNamesOutput{}, errors.New("index_min must be >= 0")
				}
				if *input.IndexMax <= *input.IndexMin {
					return TrackNamesOutput{}, errors.New("index_max must be greater than index_min")
				}
				args = append(args, int32(*input.IndexMin), int32(*input.IndexMax))
			}
			res, err := client.Query("/live/song/get/track_names", args...)
			if err != nil {
				return TrackNamesOutput{}, err
			}
			return TrackNamesOutput{TrackNames: toStringSlice(res)}, nil
		},
	)
}

func NewAbletonCreateMidiTrack(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_midi_track", "Ableton Live: create MIDI track",
		func(_ *ai.ToolContext, input CreateMidiTrackInput) (NumTracksOutput, error) {
			index := -1
			if input.Index != nil {
				index = *input.Index
				if index < -1 {
					return NumTracksOutput{}, errors.New("index must be -1 or >= 0")
				}
			}
			if err := client.Send("/live/song/create_midi_track", int32(index)); err != nil {
				return NumTracksOutput{}, err
			}
			res, err := client.Query("/live/song/get/num_tracks")
			if err != nil {
				return NumTracksOutput{}, err
			}
			if err := ensureResponseLen(res, 1); err != nil {
				return NumTracksOutput{}, err
			}
			n, err := abletonosc.AsInt(res[0])
			if err != nil {
				return NumTracksOutput{}, err
			}
			return NumTracksOutput{NumTracks: n}, nil
		},
	)
}
