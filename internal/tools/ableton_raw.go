package tools

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type RawOscSendInput struct {
	Address    string   `json:"address" jsonschema:"description=OSC address to send (e.g. /live/song/get/tempo)"`
	Args       []string `json:"args,omitempty" jsonschema:"description=Args as strings; auto-parsed to int/float/bool/string"`
	ArgsJson   string   `json:"args_json,omitempty" jsonschema:"description=Args as JSON string array (alternative to args). Format: [\"arg1\", \"arg2\"]"`
	AwaitReply *bool    `json:"await_reply,omitempty"`
	TimeoutMs  *int     `json:"timeout_ms,omitempty" jsonschema:"minimum=1,maximum=10000"`
}

type RawOscSendOutput struct {
	Address string   `json:"address"`
	Reply   []string `json:"reply,omitempty"`
}

func NewAbletonOscSend(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_osc_send", "AbletonOSC: send raw OSC message",
		func(_ *ai.ToolContext, input RawOscSendInput) (RawOscSendOutput, error) {
			if strings.TrimSpace(input.Address) == "" {
				return RawOscSendOutput{}, errors.New("address is required")
			}
			timeoutMs := 500
			if input.TimeoutMs != nil && *input.TimeoutMs > 0 {
				timeoutMs = *input.TimeoutMs
			}
			timeout := time.Duration(timeoutMs) * time.Millisecond

			// Parse args from JSON string if args array is empty but args_json is provided
			inputArgs := input.Args
			if len(inputArgs) == 0 && input.ArgsJson != "" {
				if err := json.Unmarshal([]byte(input.ArgsJson), &inputArgs); err != nil {
					return RawOscSendOutput{}, errors.New("failed to parse args_json: " + err.Error())
				}
			}

			args := make([]interface{}, 0, len(inputArgs))
			for _, s := range inputArgs {
				args = append(args, abletonosc.ParseArg(s))
			}

			await := false
			if input.AwaitReply != nil {
				await = *input.AwaitReply
			}
			if await {
				res, err := client.QueryWithTimeout(timeout, input.Address, args...)
				if err != nil {
					return RawOscSendOutput{}, err
				}
				return RawOscSendOutput{Address: input.Address, Reply: toStringSlice(res)}, nil
			}
			if err := client.Send(input.Address, args...); err != nil {
				return RawOscSendOutput{}, err
			}
			return RawOscSendOutput{Address: input.Address}, nil
		},
	)
}
