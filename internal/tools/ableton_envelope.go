package tools

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// Mixer envelope parameter indices when device_index is -1.
const (
	MixerParamVolume  = 0
	MixerParamPanning = 1
	// MixerParamSendBase + N selects send N (return A = 2, B = 3, …).
	MixerParamSendBase = 2
)

// EnvelopeStep is one automation step: hold `value` from `time` for `duration` beats.
type EnvelopeStep struct {
	Time     float64 `json:"time" jsonschema:"description=Start time in beats"`
	Duration float64 `json:"duration" jsonschema:"description=Step length in beats (use a small value e.g. 0.01 for a point)"`
	Value    float64 `json:"value" jsonschema:"description=Normalized parameter value (same scale as device/mixer params)"`
}

type EnvelopeSample struct {
	Time  float64 `json:"time"`
	Value float64 `json:"value"`
}

type ClipEnvelopeTarget struct {
	TrackIndex     int `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex      int `json:"clip_index" jsonschema:"minimum=0"`
	DeviceIndex    int `json:"device_index" jsonschema:"description=Device index, or -1 for mixer (volume/panning/sends)"`
	ParameterIndex int `json:"parameter_index" jsonschema:"description=For mixer (-1): 0=volume, 1=panning, 2+N=send N. For devices: parameter index from get_device_parameters"`
}

type GetClipEnvelopeInput struct {
	ClipEnvelopeTarget
	StartBeats *float64 `json:"start_beats,omitempty" jsonschema:"description=Sample window start (default 0)"`
	EndBeats   *float64 `json:"end_beats,omitempty" jsonschema:"description=Sample window end (default clip length)"`
	StepBeats  *float64 `json:"step_beats,omitempty" jsonschema:"description=Sample step (default 0.25 beats)"`
}

type GetClipEnvelopeOutput struct {
	TrackIndex     int              `json:"track_index"`
	ClipIndex      int              `json:"clip_index"`
	DeviceIndex    int              `json:"device_index"`
	ParameterIndex int              `json:"parameter_index"`
	ParamName      string           `json:"param_name,omitempty"`
	Exists         bool             `json:"exists"`
	Samples        []EnvelopeSample `json:"samples,omitempty" jsonschema:"description=Sampled envelope via value_at_time (Live 11 cannot list breakpoints)"`
	Hint           string           `json:"hint,omitempty"`
}

func parseEnvelopeGet(res []interface{}, deviceIndex, parameterIndex int) (GetClipEnvelopeOutput, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return GetClipEnvelopeOutput{}, err
	}
	track, _ := abletonosc.AsInt(res[0])
	clip, _ := abletonosc.AsInt(res[1])
	status := fmt.Sprint(res[2])
	out := GetClipEnvelopeOutput{
		TrackIndex:     track,
		ClipIndex:      clip,
		DeviceIndex:    deviceIndex,
		ParameterIndex: parameterIndex,
		Samples:        []EnvelopeSample{},
	}
	switch status {
	case "ok":
		if len(res) < 4 {
			return out, errors.New("envelope get missing param name")
		}
		out.Exists = true
		out.ParamName = fmt.Sprint(res[3])
		rest := res[4:]
		if len(rest)%2 != 0 {
			return out, fmt.Errorf("odd sample payload length: %d", len(rest))
		}
		for i := 0; i < len(rest); i += 2 {
			t, err := abletonosc.AsFloat64(rest[i])
			if err != nil {
				return out, err
			}
			v, err := abletonosc.AsFloat64(rest[i+1])
			if err != nil {
				return out, err
			}
			out.Samples = append(out.Samples, EnvelopeSample{Time: t, Value: v})
		}
		out.Hint = "Live 11 samples value_at_time; breakpoint lists require Live 12+."
		return out, nil
	case "missing":
		if len(res) >= 4 {
			out.ParamName = fmt.Sprint(res[3])
		}
		out.Exists = false
		out.Hint = "No envelope yet. Call ableton_set_clip_envelope_steps to create one."
		return out, nil
	case "no_clip":
		return out, actionable("no_clip", "clip slot is empty",
			"Create or select a Session clip first, then retry.")
	case "invalid_track_index", "invalid_clip_index", "invalid_device_index",
		"invalid_parameter_index", "invalid_send_index":
		return out, actionable(status, status,
			"Check track/clip/device/parameter indices (mixer uses device_index=-1).")
	default:
		detail := ""
		if len(res) > 3 {
			detail = fmt.Sprint(res[3])
		}
		return out, fmt.Errorf("envelope get failed: status=%s %s", status, detail)
	}
}

func NewAbletonGetClipEnvelope(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_clip_envelope",
		"Ableton Live: sample a Session clip automation envelope (volume/panning/send or device param). Live 11 cannot list breakpoints — returns a time/value grid via value_at_time. device_index=-1 selects mixer (0=volume, 1=panning, 2+N=send N). Requires the browser patch.",
		func(_ *ai.ToolContext, input GetClipEnvelopeInput) (GetClipEnvelopeOutput, error) {
			if err := validateEnvelopeTarget(input.ClipEnvelopeTarget); err != nil {
				return GetClipEnvelopeOutput{}, err
			}
			args := []interface{}{
				int32(input.TrackIndex), int32(input.ClipIndex),
				int32(input.DeviceIndex), int32(input.ParameterIndex),
			}
			// Optional window: only sent when start or end is provided (step alone uses defaults).
			if input.StartBeats != nil || input.EndBeats != nil {
				start := 0.0
				if input.StartBeats != nil {
					start = *input.StartBeats
				}
				end := start + 4.0
				if input.EndBeats != nil {
					end = *input.EndBeats
				}
				step := 0.25
				if input.StepBeats != nil && *input.StepBeats > 0 {
					step = *input.StepBeats
				}
				args = append(args, float32(start), float32(end), float32(step))
			}
			res, err := client.QueryWithTimeout(3*time.Second, "/live/clip/envelope/get", args...)
			if err != nil {
				return GetClipEnvelopeOutput{}, wrapActionable(err, "envelope_unavailable",
					"Install/update the browser patch and restart or hot-reload AbletonOSC, then retry.")
			}
			return parseEnvelopeGet(res, input.DeviceIndex, input.ParameterIndex)
		},
	)
}

type SetClipEnvelopeStepsInput struct {
	ClipEnvelopeTarget
	Steps []EnvelopeStep `json:"steps" jsonschema:"description=Automation steps to insert (time/duration/value)"`
	Clear bool           `json:"clear,omitempty" jsonschema:"description=If true, clear the existing envelope before inserting"`
}

type SetClipEnvelopeStepsOutput struct {
	TrackIndex     int    `json:"track_index"`
	ClipIndex      int    `json:"clip_index"`
	DeviceIndex    int    `json:"device_index"`
	ParameterIndex int    `json:"parameter_index"`
	ParamName      string `json:"param_name,omitempty"`
	StepsSet       int    `json:"steps_set"`
}

func parseEnvelopeSetSteps(res []interface{}) (SetClipEnvelopeStepsOutput, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return SetClipEnvelopeStepsOutput{}, err
	}
	track, _ := abletonosc.AsInt(res[0])
	clip, _ := abletonosc.AsInt(res[1])
	status := fmt.Sprint(res[2])
	out := SetClipEnvelopeStepsOutput{TrackIndex: track, ClipIndex: clip}
	switch status {
	case "ok":
		if len(res) >= 5 {
			n, _ := abletonosc.AsInt(res[3])
			out.StepsSet = n
			out.ParamName = fmt.Sprint(res[4])
		}
		return out, nil
	case "no_clip":
		return out, actionable("no_clip", "clip slot is empty",
			"Create a Session clip first, then retry.")
	case "error":
		where, detail := "", ""
		if len(res) > 3 {
			where = fmt.Sprint(res[3])
		}
		if len(res) > 4 {
			detail = fmt.Sprint(res[4])
		}
		msg := strings.TrimSpace(where + " " + detail)
		return out, actionable("envelope_write_failed", msg,
			"Use a Session clip (not Arrangement), ensure the parameter is on the same track, then retry.")
	default:
		return out, actionable(status, status,
			"Check track/clip/device/parameter indices (mixer uses device_index=-1).")
	}
}

func NewAbletonSetClipEnvelopeSteps(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_clip_envelope_steps",
		"Ableton Live: write Session clip automation steps (volume, panning, send, or device param). Creates the envelope if needed. Optional clear=true replaces existing points. device_index=-1 is mixer (0=volume, 1=panning, 2+N=send N). Requires the browser patch. Arrangement clips are not supported.",
		func(_ *ai.ToolContext, input SetClipEnvelopeStepsInput) (SetClipEnvelopeStepsOutput, error) {
			if err := validateEnvelopeTarget(input.ClipEnvelopeTarget); err != nil {
				return SetClipEnvelopeStepsOutput{}, err
			}
			if len(input.Steps) == 0 {
				return SetClipEnvelopeStepsOutput{}, errors.New("steps must not be empty")
			}
			clearFlag := int32(0)
			if input.Clear {
				clearFlag = 1
			}
			args := []interface{}{
				int32(input.TrackIndex), int32(input.ClipIndex),
				int32(input.DeviceIndex), int32(input.ParameterIndex),
				clearFlag,
			}
			for _, s := range input.Steps {
				if s.Duration < 0 {
					return SetClipEnvelopeStepsOutput{}, errors.New("step duration must be >= 0")
				}
				args = append(args, float32(s.Time), float32(s.Duration), float32(s.Value))
			}
			res, err := client.QueryWithTimeout(5*time.Second, "/live/clip/envelope/set_steps", args...)
			if err != nil {
				return SetClipEnvelopeStepsOutput{}, wrapActionable(err, "envelope_unavailable",
					"Install/update the browser patch and restart or hot-reload AbletonOSC, then retry.")
			}
			out, err := parseEnvelopeSetSteps(res)
			out.DeviceIndex = input.DeviceIndex
			out.ParameterIndex = input.ParameterIndex
			return out, err
		},
	)
}

type ClearClipEnvelopeInput struct {
	ClipEnvelopeTarget
	All     bool `json:"all,omitempty" jsonschema:"description=If true, clear every envelope on the clip (ignores device/parameter)"`
	Confirm bool `json:"confirm,omitempty" jsonschema:"description=Must be true to execute"`
}

type ClearClipEnvelopeOutput struct {
	TrackIndex int    `json:"track_index"`
	ClipIndex  int    `json:"clip_index"`
	Status     string `json:"status"`
	ParamName  string `json:"param_name,omitempty"`
}

func NewAbletonClearClipEnvelope(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_clear_clip_envelope",
		"Ableton Live: clear one Session clip envelope (or all with all=true). Requires confirm=true. Requires the browser patch.",
		func(_ *ai.ToolContext, input ClearClipEnvelopeInput) (ClearClipEnvelopeOutput, error) {
			if input.TrackIndex < 0 || input.ClipIndex < 0 {
				return ClearClipEnvelopeOutput{}, errors.New("track_index and clip_index must be >= 0")
			}
			summary := fmt.Sprintf("clear envelopes on clip [%d,%d]", input.TrackIndex, input.ClipIndex)
			if !input.All {
				summary = fmt.Sprintf("clear envelope device=%d param=%d on clip [%d,%d]",
					input.DeviceIndex, input.ParameterIndex, input.TrackIndex, input.ClipIndex)
			}
			if err := requireConfirm(input.Confirm, "clear_clip_envelope", summary); err != nil {
				return ClearClipEnvelopeOutput{}, err
			}
			out := ClearClipEnvelopeOutput{TrackIndex: input.TrackIndex, ClipIndex: input.ClipIndex}
			if input.All {
				res, err := client.Query("/live/clip/envelope/clear_all",
					int32(input.TrackIndex), int32(input.ClipIndex))
				if err != nil {
					return out, wrapActionable(err, "envelope_unavailable",
						"Install/update the browser patch and restart or hot-reload AbletonOSC, then retry.")
				}
				if err := ensureResponseLen(res, 3); err != nil {
					return out, err
				}
				out.Status = fmt.Sprint(res[2])
				if out.Status == "error" {
					return out, actionable("envelope_clear_failed", fmt.Sprint(res),
						"Retry on a Session clip, or clear the envelope manually in Live.")
				}
				return out, nil
			}
			if err := validateEnvelopeTarget(input.ClipEnvelopeTarget); err != nil {
				return out, err
			}
			res, err := client.Query("/live/clip/envelope/clear",
				int32(input.TrackIndex), int32(input.ClipIndex),
				int32(input.DeviceIndex), int32(input.ParameterIndex))
			if err != nil {
				return out, wrapActionable(err, "envelope_unavailable",
					"Install/update the browser patch and restart or hot-reload AbletonOSC, then retry.")
			}
			if err := ensureResponseLen(res, 3); err != nil {
				return out, err
			}
			out.Status = fmt.Sprint(res[2])
			if len(res) >= 4 {
				out.ParamName = fmt.Sprint(res[3])
			}
			if out.Status == "error" {
				return out, actionable("envelope_clear_failed", out.ParamName,
					"Retry on a Session clip, or clear the envelope manually in Live.")
			}
			return out, nil
		},
	)
}

func validateEnvelopeTarget(t ClipEnvelopeTarget) error {
	if t.TrackIndex < 0 || t.ClipIndex < 0 {
		return errors.New("track_index and clip_index must be >= 0")
	}
	if t.DeviceIndex < -1 {
		return errors.New("device_index must be >= -1 (-1 = mixer)")
	}
	if t.ParameterIndex < 0 {
		return errors.New("parameter_index must be >= 0")
	}
	return nil
}
