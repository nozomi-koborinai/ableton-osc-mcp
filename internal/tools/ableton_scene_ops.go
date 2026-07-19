package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type sceneOpsClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func querySceneNames(client oscQuerier) ([]string, error) {
	res, err := client.Query("/live/song/get/scenes/name")
	if err != nil {
		return nil, err
	}
	return toStringSlice(res), nil
}

type GetSceneNamesOutput struct {
	Scenes []SceneNameEntry `json:"scenes"`
}

type SceneNameEntry struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

func NewAbletonGetSceneNames(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_scene_names",
		"Ableton Live: list scene names with indices (Intro/Verse/Hook anchors)",
		func(_ *ai.ToolContext, _ struct{}) (GetSceneNamesOutput, error) {
			names, err := querySceneNames(client)
			if err != nil {
				return GetSceneNamesOutput{}, fmt.Errorf("get scene names: %w", err)
			}
			out := GetSceneNamesOutput{Scenes: make([]SceneNameEntry, 0, len(names))}
			for i, n := range names {
				out.Scenes = append(out.Scenes, SceneNameEntry{Index: i, Name: n})
			}
			return out, nil
		},
	)
}

type SetSceneNameInput struct {
	SceneIndex int    `json:"scene_index" jsonschema:"minimum=0"`
	Name       string `json:"name" jsonschema:"description=New scene name (e.g. Intro, Verse, Hook)"`
}

func NewAbletonSetSceneName(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_scene_name",
		"Ableton Live: rename a scene (e.g. Intro, Verse, Hook)",
		func(_ *ai.ToolContext, input SetSceneNameInput) (SentOutput, error) {
			if input.SceneIndex < 0 {
				return SentOutput{}, errors.New("scene_index must be >= 0")
			}
			name := strings.TrimSpace(input.Name)
			if name == "" {
				return SentOutput{}, errors.New("name is required")
			}
			if err := client.Send("/live/scene/set/name", int32(input.SceneIndex), name); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

type CreateNamedScenesInput struct {
	Names []string `json:"names" jsonschema:"description=Scene names to create at the end of the session (e.g. Intro, Verse, Hook, Outro)"`
}

type CreateNamedScenesOutput struct {
	Created     []SceneNameEntry `json:"created"`
	ScenesAfter int              `json:"scenes_after"`
}

func createNamedScenes(client sceneOpsClient, names []string) (CreateNamedScenesOutput, error) {
	cleaned := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			return CreateNamedScenesOutput{}, errors.New("names must not contain empty strings")
		}
		cleaned = append(cleaned, n)
	}
	if len(cleaned) == 0 {
		return CreateNamedScenesOutput{}, errors.New("names is required")
	}

	before, err := queryNumScenes(client)
	if err != nil {
		return CreateNamedScenesOutput{}, fmt.Errorf("get num scenes: %w", err)
	}

	created := make([]SceneNameEntry, 0, len(cleaned))
	for i, name := range cleaned {
		if err := client.Send("/live/song/create_scene", int32(-1)); err != nil {
			return CreateNamedScenesOutput{}, fmt.Errorf("create scene %q: %w", name, err)
		}
		index := before + i
		if err := client.Send("/live/scene/set/name", int32(index), name); err != nil {
			return CreateNamedScenesOutput{}, fmt.Errorf("name scene %q: %w", name, err)
		}
		created = append(created, SceneNameEntry{Index: index, Name: name})
	}

	after, err := queryNumScenes(client)
	if err != nil {
		return CreateNamedScenesOutput{}, fmt.Errorf("verify scenes: %w", err)
	}
	if after != before+len(cleaned) {
		return CreateNamedScenesOutput{}, fmt.Errorf("create scenes count mismatch (before=%d after=%d want=%d)", before, after, before+len(cleaned))
	}
	return CreateNamedScenesOutput{Created: created, ScenesAfter: after}, nil
}

func NewAbletonCreateNamedScenes(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_named_scenes",
		"Ableton Live: append empty named scenes (e.g. Intro, Verse, Hook) for section structure. Fill clips afterward, or use ableton_set_scene_clip_presence for subtractive arrangement from a full source scene.",
		func(_ *ai.ToolContext, input CreateNamedScenesInput) (CreateNamedScenesOutput, error) {
			return createNamedScenes(client, input.Names)
		},
	)
}

type SetSceneClipPresenceInput struct {
	SceneIndex       int   `json:"scene_index" jsonschema:"description=Target scene (clip-slot row) to show/hide clips on,minimum=0"`
	TrackIndices     []int `json:"track_indices,omitempty" jsonschema:"description=Tracks to affect; omit or empty = all tracks"`
	Present          bool  `json:"present" jsonschema:"description=false deletes clips in the scene (subtractive); true copies from source_scene_index"`
	SourceSceneIndex *int  `json:"source_scene_index,omitempty" jsonschema:"description=Required when present=true: scene to copy clips from,minimum=0"`
	Confirm          bool  `json:"confirm,omitempty" jsonschema:"description=Required when present=false (destructive delete). Omit/false returns a preview error."`
}

type SceneClipPresenceChange struct {
	TrackIndex int    `json:"track_index"`
	Action     string `json:"action" jsonschema:"description=deleted, restored, skipped_empty, skipped_already_present, skipped_no_source"`
}

type SetSceneClipPresenceOutput struct {
	SceneIndex int                       `json:"scene_index"`
	Present    bool                      `json:"present"`
	Changes    []SceneClipPresenceChange `json:"changes"`
}

func setSceneClipPresence(client sceneOpsClient, input SetSceneClipPresenceInput) (SetSceneClipPresenceOutput, error) {
	if input.SceneIndex < 0 {
		return SetSceneClipPresenceOutput{}, errors.New("scene_index must be >= 0")
	}
	if input.Present {
		if input.SourceSceneIndex == nil {
			return SetSceneClipPresenceOutput{}, errors.New("source_scene_index is required when present=true")
		}
		if *input.SourceSceneIndex < 0 {
			return SetSceneClipPresenceOutput{}, errors.New("source_scene_index must be >= 0")
		}
		if *input.SourceSceneIndex == input.SceneIndex {
			return SetSceneClipPresenceOutput{}, errors.New("source_scene_index must differ from scene_index")
		}
	} else if err := requireConfirm(input.Confirm, "clear_scene_clips",
		fmt.Sprintf("delete clips on scene %d", input.SceneIndex)); err != nil {
		return SetSceneClipPresenceOutput{}, err
	}

	numTracks, err := queryNumTracks(client)
	if err != nil {
		return SetSceneClipPresenceOutput{}, fmt.Errorf("get num tracks: %w", err)
	}
	tracks := input.TrackIndices
	if len(tracks) == 0 {
		tracks = make([]int, numTracks)
		for i := range tracks {
			tracks[i] = i
		}
	}
	for _, t := range tracks {
		if t < 0 || t >= numTracks {
			return SetSceneClipPresenceOutput{}, fmt.Errorf("track_index %d out of range (%d tracks)", t, numTracks)
		}
	}

	out := SetSceneClipPresenceOutput{
		SceneIndex: input.SceneIndex,
		Present:    input.Present,
		Changes:    make([]SceneClipPresenceChange, 0, len(tracks)),
	}

	for _, track := range tracks {
		if !input.Present {
			has, err := queryHasClip(client, track, input.SceneIndex)
			if err != nil {
				return SetSceneClipPresenceOutput{}, err
			}
			if !has {
				out.Changes = append(out.Changes, SceneClipPresenceChange{TrackIndex: track, Action: "skipped_empty"})
				continue
			}
			if err := client.Send("/live/clip_slot/delete_clip", int32(track), int32(input.SceneIndex)); err != nil {
				return SetSceneClipPresenceOutput{}, fmt.Errorf("delete clip track %d: %w", track, err)
			}
			out.Changes = append(out.Changes, SceneClipPresenceChange{TrackIndex: track, Action: "deleted"})
			continue
		}

		src := *input.SourceSceneIndex
		srcHas, err := queryHasClip(client, track, src)
		if err != nil {
			return SetSceneClipPresenceOutput{}, err
		}
		if !srcHas {
			out.Changes = append(out.Changes, SceneClipPresenceChange{TrackIndex: track, Action: "skipped_no_source"})
			continue
		}
		dstHas, err := queryHasClip(client, track, input.SceneIndex)
		if err != nil {
			return SetSceneClipPresenceOutput{}, err
		}
		if dstHas {
			out.Changes = append(out.Changes, SceneClipPresenceChange{TrackIndex: track, Action: "skipped_already_present"})
			continue
		}
		if err := client.Send("/live/clip_slot/duplicate_clip_to",
			int32(track), int32(src), int32(track), int32(input.SceneIndex),
		); err != nil {
			return SetSceneClipPresenceOutput{}, fmt.Errorf("restore clip track %d: %w", track, err)
		}
		out.Changes = append(out.Changes, SceneClipPresenceChange{TrackIndex: track, Action: "restored"})
	}
	return out, nil
}

func queryHasClip(client oscQuerier, track, clip int) (bool, error) {
	res, err := client.Query("/live/clip_slot/get/has_clip", int32(track), int32(clip))
	if err != nil {
		return false, fmt.Errorf("has_clip track %d clip %d: %w", track, clip, err)
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, err
	}
	return abletonosc.AsBool(res[2])
}

func NewAbletonSetSceneClipPresence(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_scene_clip_presence",
		"Ableton Live: subtractive arrangement helper — hide (delete) or restore clips on a scene row. present=false clears the scene; present=true copies from source_scene_index into empty slots. Use track_indices to affect a subset of tracks.",
		func(_ *ai.ToolContext, input SetSceneClipPresenceInput) (SetSceneClipPresenceOutput, error) {
			return setSceneClipPresence(client, input)
		},
	)
}
