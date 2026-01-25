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
