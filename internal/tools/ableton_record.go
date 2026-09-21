package tools

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type CreateAudioTrackInput struct {
	Index *int `json:"index,omitempty" jsonschema:"description=Track index (-1 to append),minimum=-1"`
}

type ArmTrackInput struct {
	TrackIndex int  `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	Armed      bool `json:"armed" jsonschema:"description=true to arm; false to disarm"`
}

type SetInputRoutingInput struct {
	TrackIndex  int    `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	RoutingType string `json:"routing_type" jsonschema:"description=Input routing type display name (e.g. Resampling)"`
}

type InputRoutingOutput struct {
	TrackIndex  int      `json:"track_index"`
	RoutingType string   `json:"routing_type"`
	Available   []string `json:"available,omitempty"`
}

type SetMonitoringInput struct {
	TrackIndex int `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	State      int `json:"state" jsonschema:"description=0=In 1=Auto 2=Off,minimum=0,maximum=2"`
}

type SessionRecordInput struct {
	Enabled bool `json:"enabled" jsonschema:"description=true to enable Session Record"`
}

type SessionRecordOutput struct {
	Enabled bool `json:"enabled"`
}

type BounceSessionPassInput struct {
	SceneIndices []int  `json:"scene_indices,omitempty" jsonschema:"description=Scene indices to fire in order (default Intro/Verse/Hook/Bridge/Hook = 2\\,1\\,0\\,3\\,0)"`
	BarsPerScene int    `json:"bars_per_scene,omitempty" jsonschema:"description=Bars to wait after each scene fire (default 4),minimum=1,maximum=64"`
	TrackName    string `json:"track_name,omitempty" jsonschema:"description=Bounce destination track name (default Bounce)"`
}

type BounceSessionPassOutput struct {
	OK           bool     `json:"ok"`
	TrackIndex   int      `json:"track_index"`
	TrackName    string   `json:"track_name"`
	ScenesFired  []int    `json:"scenes_fired"`
	BarsPerScene int      `json:"bars_per_scene"`
	DurationSec  float64  `json:"duration_sec"`
	RoutingType  string   `json:"routing_type"`
	FilePaths    []string `json:"file_paths" jsonschema:"description=Audio files Live wrote for this pass; pass one to ableton_analyze_local_audio to measure it"`
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

func NewAbletonArmTrack(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_arm_track", "Ableton Live: arm or disarm a track for recording",
		func(_ *ai.ToolContext, input ArmTrackInput) (SentOutput, error) {
			if input.TrackIndex < 0 {
				return SentOutput{}, errors.New("track_index must be >= 0")
			}
			val := int32(0)
			if input.Armed {
				val = 1
			}
			if err := client.Send("/live/track/set/arm", int32(input.TrackIndex), val); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonGetTrackInputRouting(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_track_input_routing", "Ableton Live: get track input routing type and available types",
		func(_ *ai.ToolContext, input TrackDevicesInput) (InputRoutingOutput, error) {
			if input.TrackIndex < 0 {
				return InputRoutingOutput{}, errors.New("track_index must be >= 0")
			}
			cur, err := client.Query("/live/track/get/input_routing_type", int32(input.TrackIndex))
			if err != nil {
				return InputRoutingOutput{}, err
			}
			if err := ensureResponseLen(cur, 2); err != nil {
				return InputRoutingOutput{}, err
			}
			avail, err := client.Query("/live/track/get/available_input_routing_types", int32(input.TrackIndex))
			if err != nil {
				return InputRoutingOutput{}, err
			}
			if err := ensureResponseLen(avail, 1); err != nil {
				return InputRoutingOutput{}, err
			}
			return InputRoutingOutput{
				TrackIndex:  input.TrackIndex,
				RoutingType: fmt.Sprint(cur[1]),
				Available:   toStringSlice(avail[1:]),
			}, nil
		},
	)
}

func NewAbletonSetTrackInputRouting(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_track_input_routing", "Ableton Live: set track input routing type (e.g. Resampling)",
		func(_ *ai.ToolContext, input SetInputRoutingInput) (SentOutput, error) {
			if input.TrackIndex < 0 {
				return SentOutput{}, errors.New("track_index must be >= 0")
			}
			if strings.TrimSpace(input.RoutingType) == "" {
				return SentOutput{}, errors.New("routing_type is required")
			}
			if err := client.Send("/live/track/set/input_routing_type", int32(input.TrackIndex), input.RoutingType); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonSetMonitoring(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_monitoring", "Ableton Live: set track monitoring state (0=In 1=Auto 2=Off)",
		func(_ *ai.ToolContext, input SetMonitoringInput) (SentOutput, error) {
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

func NewAbletonSetSessionRecord(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_session_record", "Ableton Live: enable or disable Session Record",
		func(_ *ai.ToolContext, input SessionRecordInput) (SentOutput, error) {
			val := int32(0)
			if input.Enabled {
				val = 1
			}
			if err := client.Send("/live/song/set/session_record", val); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonGetSessionRecord(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_session_record", "Ableton Live: get Session Record enabled state",
		func(_ *ai.ToolContext, _ EmptyInput) (SessionRecordOutput, error) {
			res, err := client.Query("/live/song/get/session_record")
			if err != nil {
				return SessionRecordOutput{}, err
			}
			if err := ensureResponseLen(res, 1); err != nil {
				return SessionRecordOutput{}, err
			}
			enabled, err := asBoolish(res[0])
			if err != nil {
				return SessionRecordOutput{}, err
			}
			return SessionRecordOutput{Enabled: enabled}, nil
		},
	)
}

func NewAbletonBounceSessionPass(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_bounce_session_pass",
		"Ableton Live: record a scene pass onto a Bounce audio track via Resampling and return the audio file Live wrote (leaves a Session clip; not a rendered export). Takes as long as the scenes play. Starts from a clean slate (Back to Arrangement, all clips stopped), and for the length of the pass disarms any other armed track, which it re-arms afterwards.",
		func(_ *ai.ToolContext, input BounceSessionPassInput) (BounceSessionPassOutput, error) {
			return bounceSessionPass(client, recordDeps{
				sleep: time.Sleep,
				fileSize: func(path string) (int64, error) {
					info, err := os.Stat(path)
					if err != nil {
						return 0, err
					}
					return info.Size(), nil
				},
			}, input)
		},
	)
}

func bounceSessionPass(c recordClient, deps recordDeps, input BounceSessionPassInput) (BounceSessionPassOutput, error) {
	scenes := input.SceneIndices
	if len(scenes) == 0 {
		scenes = []int{2, 1, 0, 3, 0} // Intro, Verse, Hook, Bridge, Hook
	}
	bars := input.BarsPerScene
	if bars <= 0 {
		bars = 4
	}
	trackName := strings.TrimSpace(input.TrackName)
	if trackName == "" {
		trackName = "Bounce"
	}

	spans := make([]recordSpan, 0, len(scenes))
	for i := range scenes {
		spans = append(spans, recordSpan{SceneIndex: &scenes[i], Bars: bars})
	}
	take, err := recordResampledPass(c, deps, recordPlan{TrackName: trackName, Spans: spans, StopClipsAtEnds: true})
	if err != nil {
		return BounceSessionPassOutput{}, err
	}
	return BounceSessionPassOutput{
		OK:           true,
		TrackIndex:   take.TrackIndex,
		TrackName:    trackName,
		ScenesFired:  scenes,
		BarsPerScene: bars,
		DurationSec:  take.WindowBeats * 60 / take.TempoBPM, // what was recorded, bar to bar
		RoutingType:  take.RoutingType,
		FilePaths:    take.FilePaths,
	}, nil
}

func ensureNamedAudioTrack(client recordClient, name string) (int, error) {
	namesRes, err := client.Query("/live/song/get/track_names")
	if err != nil {
		return -1, err
	}
	names := toStringSlice(namesRes)
	for i, n := range names {
		if n == name {
			return i, nil
		}
	}
	if err := client.Send("/live/song/create_audio_track", int32(-1)); err != nil {
		return -1, err
	}
	time.Sleep(150 * time.Millisecond)
	namesRes, err = client.Query("/live/song/get/track_names")
	if err != nil {
		return -1, err
	}
	names = toStringSlice(namesRes)
	if len(names) == 0 {
		return -1, errors.New("no tracks after create_audio_track")
	}
	idx := len(names) - 1
	if err := client.Send("/live/track/set/name", int32(idx), name); err != nil {
		return -1, err
	}
	return idx, nil
}

func pickResamplingRouting(client recordClient, trackIndex int) (string, error) {
	avail, err := client.Query("/live/track/get/available_input_routing_types", int32(trackIndex))
	if err != nil {
		return "", err
	}
	if err := ensureResponseLen(avail, 1); err != nil {
		return "", err
	}
	types := toStringSlice(avail[1:])
	for _, t := range types {
		if strings.EqualFold(t, "Resampling") {
			return t, nil
		}
	}
	for _, t := range types {
		if strings.Contains(strings.ToLower(t), "resampling") {
			return t, nil
		}
	}
	return "", fmt.Errorf("resampling input not available on track %d; available=%v", trackIndex, types)
}

func asBoolish(v interface{}) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case int32:
		return x != 0, nil
	case int64:
		return x != 0, nil
	case int:
		return x != 0, nil
	case float64:
		return x != 0, nil
	case float32:
		return x != 0, nil
	default:
		s := strings.ToLower(fmt.Sprint(v))
		if s == "true" || s == "1" {
			return true, nil
		}
		if s == "false" || s == "0" {
			return false, nil
		}
		return false, fmt.Errorf("cannot parse bool from %v (%T)", v, v)
	}
}
