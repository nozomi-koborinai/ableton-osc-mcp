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

type SetSceneNameInput struct {
	SceneIndex int    `json:"scene_index" jsonschema:"minimum=0"`
	Name       string `json:"name" jsonschema:"description=New scene name"`
}

type CreateSceneInput struct {
	SceneIndex *int `json:"scene_index,omitempty" jsonschema:"description=Index to insert scene at (-1 to append),minimum=-1"`
}

type NumScenesOutput struct {
	NumScenes int `json:"num_scenes"`
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

func NewAbletonSetSceneName(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_scene_name", "Ableton Live: set scene name",
		func(_ *ai.ToolContext, input SetSceneNameInput) (SentOutput, error) {
			if input.SceneIndex < 0 {
				return SentOutput{}, errors.New("scene_index must be >= 0")
			}
			if err := client.Send("/live/scene/set/name", int32(input.SceneIndex), input.Name); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonCreateScene(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_scene", "Ableton Live: create a new scene",
		func(_ *ai.ToolContext, input CreateSceneInput) (NumScenesOutput, error) {
			index := -1
			if input.SceneIndex != nil {
				index = *input.SceneIndex
				if index < -1 {
					return NumScenesOutput{}, errors.New("scene_index must be -1 or >= 0")
				}
			}
			if err := client.Send("/live/song/create_scene", int32(index)); err != nil {
				return NumScenesOutput{}, err
			}
			res, err := client.Query("/live/song/get/num_scenes")
			if err != nil {
				return NumScenesOutput{}, err
			}
			if err := ensureResponseLen(res, 1); err != nil {
				return NumScenesOutput{}, err
			}
			n, err := abletonosc.AsInt(res[0])
			if err != nil {
				return NumScenesOutput{}, err
			}
			return NumScenesOutput{NumScenes: n}, nil
		},
	)
}

func NewAbletonDuplicateScene(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_duplicate_scene", "Ableton Live: duplicate a scene",
		func(_ *ai.ToolContext, input FireSceneInput) (NumScenesOutput, error) {
			if input.SceneIndex < 0 {
				return NumScenesOutput{}, errors.New("scene_index must be >= 0")
			}
			if err := client.Send("/live/song/duplicate_scene", int32(input.SceneIndex)); err != nil {
				return NumScenesOutput{}, err
			}
			res, err := client.Query("/live/song/get/num_scenes")
			if err != nil {
				return NumScenesOutput{}, err
			}
			if err := ensureResponseLen(res, 1); err != nil {
				return NumScenesOutput{}, err
			}
			n, err := abletonosc.AsInt(res[0])
			if err != nil {
				return NumScenesOutput{}, err
			}
			return NumScenesOutput{NumScenes: n}, nil
		},
	)
}
