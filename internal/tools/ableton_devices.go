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

type GetDeviceParametersInput struct {
	TrackIndex  int `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int `json:"device_index" jsonschema:"minimum=0"`
}

type DeviceParameter struct {
	Index        int     `json:"index"`
	Name         string  `json:"name"`
	Value        float64 `json:"value"`
	DisplayValue string  `json:"display_value,omitempty" jsonschema:"description=Human-readable value with units or enum name (e.g. 37.0 Hz, 1/2, Ins); requires the browser patch"`
	Min          float64 `json:"min"`
	Max          float64 `json:"max"`
	IsQuantized  *bool   `json:"is_quantized,omitempty" jsonschema:"description=true for stepped/enum parameters such as filter type or mix mode"`
}

type DeviceParametersOutput struct {
	TrackIndex  int               `json:"track_index"`
	DeviceIndex int               `json:"device_index"`
	Parameters  []DeviceParameter `json:"parameters"`
}

type SetDeviceParameterInput struct {
	TrackIndex     int     `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex    int     `json:"device_index" jsonschema:"minimum=0"`
	ParameterIndex int     `json:"parameter_index" jsonschema:"description=Parameter index (from get_device_parameters),minimum=0"`
	Value          float64 `json:"value" jsonschema:"description=Parameter value to set"`
}

// buildDeviceParameters merges the parallel parameter lists returned by AbletonOSC
// into structured parameters. displays and quantized are best-effort: they may be
// empty (patch missing) or shorter than names, in which case those fields are left
// unset. values/mins/maxs must all match names in length.
func buildDeviceParameters(names []string, values, mins, maxs, displays, quantized []interface{}) ([]DeviceParameter, error) {
	if len(values) != len(names) || len(mins) != len(names) || len(maxs) != len(names) {
		return nil, fmt.Errorf("parameter list length mismatch: names=%d values=%d mins=%d maxs=%d",
			len(names), len(values), len(mins), len(maxs))
	}
	params := make([]DeviceParameter, 0, len(names))
	for i := range names {
		val, err := abletonosc.AsFloat64(values[i])
		if err != nil {
			return nil, err
		}
		mn, err := abletonosc.AsFloat64(mins[i])
		if err != nil {
			return nil, err
		}
		mx, err := abletonosc.AsFloat64(maxs[i])
		if err != nil {
			return nil, err
		}
		param := DeviceParameter{Index: i, Name: names[i], Value: val, Min: mn, Max: mx}
		if i < len(displays) {
			param.DisplayValue = fmt.Sprint(displays[i])
		}
		if i < len(quantized) {
			if q, err := abletonosc.AsBool(quantized[i]); err == nil {
				param.IsQuantized = &q
			}
		}
		params = append(params, param)
	}
	return params, nil
}

func NewAbletonGetDeviceParameters(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_device_parameters", "Ableton Live: get all parameters of a device on a track, including human-readable display values (units/enum names) when the browser patch is installed",
		func(_ *ai.ToolContext, input GetDeviceParametersInput) (DeviceParametersOutput, error) {
			if input.TrackIndex < 0 {
				return DeviceParametersOutput{}, errors.New("track_index must be >= 0")
			}
			if input.DeviceIndex < 0 {
				return DeviceParametersOutput{}, errors.New("device_index must be >= 0")
			}

			args := []interface{}{int32(input.TrackIndex), int32(input.DeviceIndex)}

			namesRes, err := client.Query("/live/device/get/parameters/name", args...)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			if err := ensureResponseLen(namesRes, 2); err != nil {
				return DeviceParametersOutput{}, err
			}
			names := toStringSlice(namesRes[2:])

			valuesRes, err := client.Query("/live/device/get/parameters/value", args...)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			if err := ensureResponseLen(valuesRes, 2); err != nil {
				return DeviceParametersOutput{}, err
			}

			minsRes, err := client.Query("/live/device/get/parameters/min", args...)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			if err := ensureResponseLen(minsRes, 2); err != nil {
				return DeviceParametersOutput{}, err
			}

			maxsRes, err := client.Query("/live/device/get/parameters/max", args...)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			if err := ensureResponseLen(maxsRes, 2); err != nil {
				return DeviceParametersOutput{}, err
			}

			values := valuesRes[2:]
			mins := minsRes[2:]
			maxs := maxsRes[2:]

			// Display strings (e.g. "37.0 Hz", "1/2", "Ins") come from the browser
			// patch's /live/device/get/parameters/value_string. is_quantized is stock
			// AbletonOSC. Both are best-effort so numeric params still return without them.
			var displays, quantized []interface{}
			if dispRes, err := client.Query("/live/device/get/parameters/value_string", args...); err == nil && len(dispRes) >= 2 {
				displays = dispRes[2:]
			}
			if quantRes, err := client.Query("/live/device/get/parameters/is_quantized", args...); err == nil && len(quantRes) >= 2 {
				quantized = quantRes[2:]
			}

			params, err := buildDeviceParameters(names, values, mins, maxs, displays, quantized)
			if err != nil {
				return DeviceParametersOutput{}, err
			}

			return DeviceParametersOutput{
				TrackIndex:  input.TrackIndex,
				DeviceIndex: input.DeviceIndex,
				Parameters:  params,
			}, nil
		},
	)
}

func NewAbletonSetDeviceParameter(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_device_parameter", "Ableton Live: set a device parameter value",
		func(_ *ai.ToolContext, input SetDeviceParameterInput) (SentOutput, error) {
			if input.TrackIndex < 0 {
				return SentOutput{}, errors.New("track_index must be >= 0")
			}
			if input.DeviceIndex < 0 {
				return SentOutput{}, errors.New("device_index must be >= 0")
			}
			if input.ParameterIndex < 0 {
				return SentOutput{}, errors.New("parameter_index must be >= 0")
			}
			if err := client.Send("/live/device/set/parameter/value",
				int32(input.TrackIndex),
				int32(input.DeviceIndex),
				int32(input.ParameterIndex),
				float32(input.Value),
			); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

type SetDeviceParameterStringInput struct {
	TrackIndex     int    `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex    int    `json:"device_index" jsonschema:"minimum=0"`
	ParameterIndex int    `json:"parameter_index" jsonschema:"description=Parameter index (from get_device_parameters),minimum=0"`
	Value          string `json:"value" jsonschema:"description=Human-readable value: enum name (e.g. Ins) or numeric with unit (e.g. 180 Hz, -3.5 dB, 50 %)"`
}

type SetDeviceParameterStringOutput struct {
	TrackIndex     int      `json:"track_index"`
	DeviceIndex    int      `json:"device_index"`
	ParameterIndex int      `json:"parameter_index"`
	Status         string   `json:"status" jsonschema:"description=set, or no_match / invalid_* on failure"`
	Value          float64  `json:"value,omitempty" jsonschema:"description=Resolved numeric value when status is set"`
	DisplayValue   string   `json:"display_value,omitempty" jsonschema:"description=Resolved human-readable value when status is set"`
	Options        []string `json:"options,omitempty" jsonschema:"description=Available enum options when status is no_match"`
}

func parseSetDeviceParameterStringResponse(res []interface{}) (SetDeviceParameterStringOutput, error) {
	if err := ensureResponseLen(res, 4); err != nil {
		return SetDeviceParameterStringOutput{}, err
	}
	trackIndex, err := abletonosc.AsInt(res[0])
	if err != nil {
		return SetDeviceParameterStringOutput{}, fmt.Errorf("parse track_index: %w", err)
	}
	deviceIndex, err := abletonosc.AsInt(res[1])
	if err != nil {
		return SetDeviceParameterStringOutput{}, fmt.Errorf("parse device_index: %w", err)
	}
	parameterIndex, err := abletonosc.AsInt(res[2])
	if err != nil {
		return SetDeviceParameterStringOutput{}, fmt.Errorf("parse parameter_index: %w", err)
	}
	status := fmt.Sprint(res[3])
	out := SetDeviceParameterStringOutput{
		TrackIndex:     trackIndex,
		DeviceIndex:    deviceIndex,
		ParameterIndex: parameterIndex,
		Status:         status,
	}
	switch status {
	case "set":
		if len(res) >= 6 {
			if v, err := abletonosc.AsFloat64(res[4]); err == nil {
				out.Value = v
			}
			out.DisplayValue = fmt.Sprint(res[5])
		}
		return out, nil
	case "no_match":
		target := ""
		if len(res) > 4 {
			target = fmt.Sprint(res[4])
		}
		if len(res) > 5 {
			out.Options = toStringSlice(res[5:])
		}
		if len(out.Options) > 0 {
			return out, fmt.Errorf("no value matched %q; options: %s", target, strings.Join(out.Options, ", "))
		}
		return out, fmt.Errorf("no value matched %q for parameter index %d", target, parameterIndex)
	default:
		return out, fmt.Errorf("set parameter string failed: status=%s reply=%v", status, res)
	}
}

func NewAbletonSetDeviceParameterString(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_device_parameter_string",
		"Ableton Live: set a device parameter from a human-readable string such as an enum name (Ins) or a numeric value with unit (180 Hz, -3.5 dB, 50 %); requires the browser patch",
		func(_ *ai.ToolContext, input SetDeviceParameterStringInput) (SetDeviceParameterStringOutput, error) {
			if input.TrackIndex < 0 {
				return SetDeviceParameterStringOutput{}, errors.New("track_index must be >= 0")
			}
			if input.DeviceIndex < 0 {
				return SetDeviceParameterStringOutput{}, errors.New("device_index must be >= 0")
			}
			if input.ParameterIndex < 0 {
				return SetDeviceParameterStringOutput{}, errors.New("parameter_index must be >= 0")
			}
			value := strings.TrimSpace(input.Value)
			if value == "" {
				return SetDeviceParameterStringOutput{}, errors.New("value is required")
			}
			res, err := client.QueryWithTimeout(
				3*time.Second,
				"/live/device/set/parameter/string",
				int32(input.TrackIndex),
				int32(input.DeviceIndex),
				int32(input.ParameterIndex),
				value,
			)
			if err != nil {
				return SetDeviceParameterStringOutput{}, fmt.Errorf("set parameter string failed (browser patch required): %w", err)
			}
			return parseSetDeviceParameterStringResponse(res)
		},
	)
}

type DeleteDeviceInput struct {
	TrackIndex  int  `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int  `json:"device_index" jsonschema:"description=Device index on the track (from get_track_devices),minimum=0"`
	Confirm     bool `json:"confirm,omitempty" jsonschema:"description=Must be true to execute; omit/false returns a preview error without deleting. Prefer ableton_preview_destructive first."`
}

type DeleteDeviceOutput struct {
	TrackIndex    int    `json:"track_index"`
	DeviceIndex   int    `json:"device_index"`
	Status        string `json:"status" jsonschema:"description=deleted, or invalid_track_index / invalid_device_index / error on failure"`
	DeviceName    string `json:"device_name,omitempty" jsonschema:"description=Name of the deleted device"`
	DevicesBefore *int   `json:"devices_before,omitempty" jsonschema:"description=Device count before deletion"`
	DevicesAfter  *int   `json:"devices_after,omitempty" jsonschema:"description=Device count after deletion; confirms success"`
}

func parseDeleteDeviceResponse(res []interface{}) (DeleteDeviceOutput, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return DeleteDeviceOutput{}, err
	}
	trackIndex, err := abletonosc.AsInt(res[0])
	if err != nil {
		return DeleteDeviceOutput{}, fmt.Errorf("parse track_index: %w", err)
	}
	deviceIndex, err := abletonosc.AsInt(res[1])
	if err != nil {
		return DeleteDeviceOutput{}, fmt.Errorf("parse device_index: %w", err)
	}
	status := fmt.Sprint(res[2])
	out := DeleteDeviceOutput{
		TrackIndex:  trackIndex,
		DeviceIndex: deviceIndex,
		Status:      status,
	}
	if status != "deleted" {
		detail := ""
		if len(res) > 3 {
			detail = fmt.Sprint(res[3])
		}
		if detail != "" {
			return out, fmt.Errorf("delete device failed: status=%s detail=%s", status, detail)
		}
		return out, fmt.Errorf("delete device failed: status=%s", status)
	}
	if len(res) >= 6 {
		out.DeviceName = fmt.Sprint(res[3])
		if b, err := abletonosc.AsInt(res[4]); err == nil {
			out.DevicesBefore = &b
		}
		if a, err := abletonosc.AsInt(res[5]); err == nil {
			out.DevicesAfter = &a
		}
	}
	return out, nil
}

func NewAbletonDeleteDevice(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_delete_device",
		"Ableton Live: delete a device from a track by index. Requires confirm=true. Returns device_name and devices_before/after so success is confirmed instead of fire-and-forget; requires the browser patch. To replace a device, delete then load the new one",
		func(_ *ai.ToolContext, input DeleteDeviceInput) (DeleteDeviceOutput, error) {
			if input.TrackIndex < 0 {
				return DeleteDeviceOutput{}, errors.New("track_index must be >= 0")
			}
			if input.DeviceIndex < 0 {
				return DeleteDeviceOutput{}, errors.New("device_index must be >= 0")
			}
			if err := requireConfirm(input.Confirm, "delete_device",
				fmt.Sprintf("device %d on track %d", input.DeviceIndex, input.TrackIndex)); err != nil {
				return DeleteDeviceOutput{}, err
			}
			res, err := client.QueryWithTimeout(
				5*time.Second,
				"/live/device/delete",
				int32(input.TrackIndex),
				int32(input.DeviceIndex),
			)
			if err != nil {
				return DeleteDeviceOutput{}, wrapActionable(err, "delete_device_failed",
					"Install/update the browser patch and restart Live, or delete the device manually in Live.")
			}
			out, perr := parseDeleteDeviceResponse(res)
			if perr != nil {
				return out, wrapActionable(perr, "delete_device_failed",
					"Call ableton_get_track_devices to verify indices, then retry with confirm=true.")
			}
			return out, nil
		},
	)
}
