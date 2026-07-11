package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type AbletonTestOutput struct {
	OK string `json:"ok" jsonschema:"description=Should be 'ok' when AbletonOSC is reachable"`
}

type SentOutput struct {
	Sent bool `json:"sent"`
}

type TempoOutput struct {
	TempoBPM float64 `json:"tempo_bpm"`
}

type SetTempoInput struct {
	TempoBPM float64 `json:"tempo_bpm" jsonschema:"description=Tempo in BPM,minimum=10,maximum=400"`
}

type StopAllClipsOutput struct {
	Stopped bool `json:"stopped"`
}

type SetSongKeyInput struct {
	RootNote  int    `json:"root_note" jsonschema:"description=Root note (0=C 1=C# 2=D ... 11=B),minimum=0,maximum=11"`
	ScaleName string `json:"scale_name" jsonschema:"description=Scale name (e.g. Major Minor Dorian Mixolydian Pentatonic)"`
}

type SongKeyOutput struct {
	RootNote  int    `json:"root_note"`
	ScaleName string `json:"scale_name"`
}

type PlayingOutput struct {
	IsPlaying bool `json:"is_playing"`
}

type MetronomeInput struct {
	Enabled bool `json:"enabled" jsonschema:"description=true to enable metronome; false to disable"`
}

type MetronomeOutput struct {
	Enabled bool `json:"enabled"`
}

func NewAbletonTest(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_test", "AbletonOSC: test connection",
		func(_ *ai.ToolContext, _ EmptyInput) (AbletonTestOutput, error) {
			res, err := client.Query("/live/test")
			if err != nil {
				return AbletonTestOutput{}, err
			}
			ok := "ok"
			if len(res) > 0 {
				ok = fmt.Sprint(res[0])
			}
			return AbletonTestOutput{OK: ok}, nil
		},
	)
}

func NewAbletonGetTempo(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_tempo", "Ableton Live: get tempo",
		func(_ *ai.ToolContext, _ EmptyInput) (TempoOutput, error) {
			res, err := client.Query("/live/song/get/tempo")
			if err != nil {
				return TempoOutput{}, err
			}
			if err := ensureResponseLen(res, 1); err != nil {
				return TempoOutput{}, err
			}
			tempo, err := abletonosc.AsFloat64(res[0])
			if err != nil {
				return TempoOutput{}, err
			}
			return TempoOutput{TempoBPM: tempo}, nil
		},
	)
}

func NewAbletonPlay(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_play", "Ableton Live: start playback",
		func(_ *ai.ToolContext, _ EmptyInput) (PlayingOutput, error) {
			if err := client.Send("/live/song/start_playing"); err != nil {
				return PlayingOutput{}, err
			}
			return PlayingOutput{IsPlaying: true}, nil
		},
	)
}

func NewAbletonStop(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_stop", "Ableton Live: stop playback",
		func(_ *ai.ToolContext, _ EmptyInput) (PlayingOutput, error) {
			if err := client.Send("/live/song/stop_playing"); err != nil {
				return PlayingOutput{}, err
			}
			return PlayingOutput{IsPlaying: false}, nil
		},
	)
}

func NewAbletonStopAllClips(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_stop_all_clips", "Ableton Live: stop all playing clips",
		func(_ *ai.ToolContext, _ EmptyInput) (StopAllClipsOutput, error) {
			if err := client.Send("/live/song/stop_all_clips"); err != nil {
				return StopAllClipsOutput{}, err
			}
			return StopAllClipsOutput{Stopped: true}, nil
		},
	)
}

func NewAbletonSetSongKey(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_song_key", "Ableton Live: set song root note and scale (0=C 1=C# 2=D ... 11=B)",
		func(_ *ai.ToolContext, input SetSongKeyInput) (SongKeyOutput, error) {
			if input.RootNote < 0 || input.RootNote > 11 {
				return SongKeyOutput{}, errors.New("root_note must be 0-11 (0=C, 1=C#, 2=D, ... 11=B)")
			}
			if strings.TrimSpace(input.ScaleName) == "" {
				return SongKeyOutput{}, errors.New("scale_name is required")
			}
			if err := client.Send("/live/song/set/root_note", int32(input.RootNote)); err != nil {
				return SongKeyOutput{}, err
			}
			if err := client.Send("/live/song/set/scale_name", input.ScaleName); err != nil {
				return SongKeyOutput{}, err
			}
			return SongKeyOutput(input), nil
		},
	)
}

func NewAbletonSetMetronome(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_metronome", "Ableton Live: enable or disable metronome",
		func(_ *ai.ToolContext, input MetronomeInput) (MetronomeOutput, error) {
			val := int32(0)
			if input.Enabled {
				val = 1
			}
			if err := client.Send("/live/song/set/metronome", val); err != nil {
				return MetronomeOutput{}, err
			}
			return MetronomeOutput(input), nil
		},
	)
}

func NewAbletonSetTempo(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_tempo", "Ableton Live: set tempo",
		func(_ *ai.ToolContext, input SetTempoInput) (TempoOutput, error) {
			if input.TempoBPM <= 0 {
				return TempoOutput{}, errors.New("tempo_bpm must be positive")
			}
			if err := client.Send("/live/song/set/tempo", float32(input.TempoBPM)); err != nil {
				return TempoOutput{}, err
			}
			res, err := client.Query("/live/song/get/tempo")
			if err != nil {
				return TempoOutput{}, err
			}
			if err := ensureResponseLen(res, 1); err != nil {
				return TempoOutput{}, err
			}
			tempo, err := abletonosc.AsFloat64(res[0])
			if err != nil {
				return TempoOutput{}, err
			}
			return TempoOutput{TempoBPM: tempo}, nil
		},
	)
}
