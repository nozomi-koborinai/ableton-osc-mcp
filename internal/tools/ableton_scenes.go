package tools

import (
	"errors"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type FireSceneInput struct {
	SceneIndex int `json:"scene_index" jsonschema:"minimum=0"`
}

func NewAbletonFireScene(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_fire_scene", "Ableton Live: fire (launch) a scene",
		func(_ *ai.ToolContext, input FireSceneInput) (FiredOutput, error) {
			if input.SceneIndex < 0 {
				return FiredOutput{}, errors.New("scene_index must be >= 0")
			}
			if err := client.Send("/live/scene/fire", int32(input.SceneIndex)); err != nil {
				return FiredOutput{}, err
			}
			return FiredOutput{Fired: true}, nil
		},
	)
}
