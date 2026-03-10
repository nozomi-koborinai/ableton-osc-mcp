package tools

import (
	"errors"
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type GetDeviceParametersInput struct {
	TrackIndex  int `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int `json:"device_index" jsonschema:"minimum=0"`
}

type DeviceParameter struct {
	Index int     `json:"index"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
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

func NewAbletonGetDeviceParameters(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_device_parameters", "Ableton Live: get all parameters of a device on a track",
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

			if len(names) != len(values) || len(names) != len(mins) || len(names) != len(maxs) {
				return DeviceParametersOutput{}, fmt.Errorf("parameter list length mismatch: names=%d values=%d mins=%d maxs=%d",
					len(names), len(values), len(mins), len(maxs))
			}

			params := make([]DeviceParameter, 0, len(names))
			for i := range names {
				val, err := abletonosc.AsFloat64(values[i])
				if err != nil {
					return DeviceParametersOutput{}, err
				}
				min, err := abletonosc.AsFloat64(mins[i])
				if err != nil {
					return DeviceParametersOutput{}, err
				}
				max, err := abletonosc.AsFloat64(maxs[i])
				if err != nil {
					return DeviceParametersOutput{}, err
				}
				params = append(params, DeviceParameter{
					Index: i,
					Name:  names[i],
					Value: val,
					Min:   min,
					Max:   max,
				})
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
