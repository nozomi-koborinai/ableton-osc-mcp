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

type AbletonShowMessageInput struct {
	Message string `json:"message" jsonschema:"description=Message to show in Live status bar"`
}

type SentOutput struct {
	Sent bool `json:"sent"`
}

type AbletonVersionOutput struct {
	MajorVersion int `json:"major_version"`
	MinorVersion int `json:"minor_version"`
}

type TempoOutput struct {
	TempoBPM float64 `json:"tempo_bpm"`
}

type SetTempoInput struct {
	TempoBPM float64 `json:"tempo_bpm" jsonschema:"description=Tempo in BPM,minimum=10,maximum=400"`
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

func NewAbletonShowMessage(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_show_message", "Ableton Live: show status message",
		func(_ *ai.ToolContext, input AbletonShowMessageInput) (SentOutput, error) {
			if strings.TrimSpace(input.Message) == "" {
				return SentOutput{}, errors.New("message is required")
			}
			if err := client.Send("/live/api/show_message", input.Message); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonGetVersion(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_version", "Ableton Live: get version",
		func(_ *ai.ToolContext, _ EmptyInput) (AbletonVersionOutput, error) {
			res, err := client.Query("/live/application/get/version")
			if err != nil {
				return AbletonVersionOutput{}, err
			}
			if err := ensureResponseLen(res, 2); err != nil {
				return AbletonVersionOutput{}, err
			}
			major, err := abletonosc.AsInt(res[0])
			if err != nil {
				return AbletonVersionOutput{}, err
			}
			minor, err := abletonosc.AsInt(res[1])
			if err != nil {
				return AbletonVersionOutput{}, err
			}
			return AbletonVersionOutput{MajorVersion: major, MinorVersion: minor}, nil
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

type CreateAudioTrackInput struct {
	Index *int `json:"index,omitempty" jsonschema:"description=Track index (-1 to append),minimum=-1"`
}

type MonitoringInput struct {
	TrackIndex int `json:"track_index" jsonschema:"minimum=0"`
	State      int `json:"state" jsonschema:"description=0=In 1=Auto 2=Off,minimum=0,maximum=2"`
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
			return SongKeyOutput{RootNote: input.RootNote, ScaleName: input.ScaleName}, nil
		},
	)
}

func NewAbletonSessionRecord(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_session_record", "Ableton Live: trigger session recording",
		func(_ *ai.ToolContext, _ EmptyInput) (SentOutput, error) {
			if err := client.Send("/live/song/trigger_session_record"); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonCaptureMidi(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_capture_midi", "Ableton Live: capture MIDI (retroactively capture what was just played)",
		func(_ *ai.ToolContext, _ EmptyInput) (SentOutput, error) {
			if err := client.Send("/live/song/capture_midi"); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
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
			return MetronomeOutput{Enabled: input.Enabled}, nil
		},
	)
}

func NewAbletonCreateAudioTrack(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_audio_track", "Ableton Live: create audio track",
		func(_ *ai.ToolContext, input CreateAudioTrackInput) (NumTracksOutput, error) {
			index := -1
			if input.Index != nil {
				index = *input.Index
				if index < -1 {
					return NumTracksOutput{}, errors.New("index must be -1 or >= 0")
				}
			}
			if err := client.Send("/live/song/create_audio_track", int32(index)); err != nil {
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

func NewAbletonSetMonitoring(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_monitoring", "Ableton Live: set track monitoring state (0=In, 1=Auto, 2=Off)",
		func(_ *ai.ToolContext, input MonitoringInput) (SentOutput, error) {
			if input.TrackIndex < 0 {
				return SentOutput{}, errors.New("track_index must be >= 0")
			}
			if input.State < 0 || input.State > 2 {
				return SentOutput{}, errors.New("state must be 0 (In), 1 (Auto), or 2 (Off)")
			}
			if err := client.Send("/live/track/set/current_monitoring_state", int32(input.TrackIndex), int32(input.State)); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
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
