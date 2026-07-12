package tools

import (
	"errors"
	"fmt"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type TrackMeterInput struct {
	TrackIndex int `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
}

type MeterOutput struct {
	TrackIndex *int    `json:"track_index,omitempty"`
	Level      float64 `json:"level" jsonschema:"description=Combined output meter level (0.0-1.0)"`
	Left       float64 `json:"left"`
	Right      float64 `json:"right"`
}

type MasterVolumeOutput struct {
	Volume float64 `json:"volume" jsonschema:"description=Master volume (0.0=silence, ~0.85=0dB)"`
}

type SetMasterVolumeInput struct {
	Volume float64 `json:"volume" jsonschema:"description=Master volume (0.0=silence, ~0.85=0dB),minimum=0,maximum=1"`
}

type MasterDevicesOutput struct {
	Devices []TrackDevice `json:"devices"`
}

type MasterDeviceParametersInput struct {
	DeviceIndex int `json:"device_index" jsonschema:"minimum=0"`
}

type SetMasterDeviceParameterInput struct {
	DeviceIndex    int     `json:"device_index" jsonschema:"minimum=0"`
	ParameterIndex int     `json:"parameter_index" jsonschema:"minimum=0"`
	Value          float64 `json:"value"`
}

type LoadOnMasterInput struct {
	RootName  string   `json:"root_name" jsonschema:"description=Browser root (e.g. Audio Effects)"`
	PathParts []string `json:"path_parts,omitempty" jsonschema:"description=Optional folder path under root"`
	ItemName  string   `json:"item_name" jsonschema:"description=Preset/device filename (e.g. Limiter.adv)"`
}

type LoadOnMasterOutput struct {
	Loaded        string `json:"loaded"`
	DevicesBefore int    `json:"devices_before"`
	DevicesAfter  int    `json:"devices_after"`
}

func NewAbletonGetTrackMeter(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_track_meter", "Ableton Live: get track output meter levels",
		func(_ *ai.ToolContext, input TrackMeterInput) (MeterOutput, error) {
			if input.TrackIndex < 0 {
				return MeterOutput{}, errors.New("track_index must be >= 0")
			}
			idx := int32(input.TrackIndex)
			level, err := queryMeter(client, "/live/track/get/output_meter_level", idx)
			if err != nil {
				return MeterOutput{}, err
			}
			left, err := queryMeter(client, "/live/track/get/output_meter_left", idx)
			if err != nil {
				return MeterOutput{}, err
			}
			right, err := queryMeter(client, "/live/track/get/output_meter_right", idx)
			if err != nil {
				return MeterOutput{}, err
			}
			ti := input.TrackIndex
			return MeterOutput{TrackIndex: &ti, Level: level, Left: left, Right: right}, nil
		},
	)
}

func NewAbletonGetMasterMeter(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_master_meter", "Ableton Live: get master output meter levels (requires master patch)",
		func(_ *ai.ToolContext, _ struct{}) (MeterOutput, error) {
			level, err := queryMeter(client, "/live/master/get/output_meter_level")
			if err != nil {
				return MeterOutput{}, err
			}
			left, err := queryMeter(client, "/live/master/get/output_meter_left")
			if err != nil {
				return MeterOutput{}, err
			}
			right, err := queryMeter(client, "/live/master/get/output_meter_right")
			if err != nil {
				return MeterOutput{}, err
			}
			return MeterOutput{Level: level, Left: left, Right: right}, nil
		},
	)
}

func NewAbletonGetMasterVolume(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_master_volume", "Ableton Live: get master volume (requires master patch)",
		func(_ *ai.ToolContext, _ struct{}) (MasterVolumeOutput, error) {
			res, err := client.Query("/live/master/get/volume")
			if err != nil {
				return MasterVolumeOutput{}, err
			}
			if err := ensureResponseLen(res, 1); err != nil {
				return MasterVolumeOutput{}, err
			}
			v, err := abletonosc.AsFloat64(res[0])
			if err != nil {
				return MasterVolumeOutput{}, err
			}
			return MasterVolumeOutput{Volume: v}, nil
		},
	)
}

func NewAbletonSetMasterVolume(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_master_volume", "Ableton Live: set master volume (requires master patch)",
		func(_ *ai.ToolContext, input SetMasterVolumeInput) (SentOutput, error) {
			if input.Volume < 0 || input.Volume > 1 {
				return SentOutput{}, errors.New("volume must be between 0 and 1")
			}
			if err := client.Send("/live/master/set/volume", float32(input.Volume)); err != nil {
				return SentOutput{}, err
			}
			return SentOutput{Sent: true}, nil
		},
	)
}

func NewAbletonGetMasterDevices(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_master_devices", "Ableton Live: list devices on master track (requires master patch)",
		func(_ *ai.ToolContext, _ struct{}) (MasterDevicesOutput, error) {
			namesRes, err := client.Query("/live/master/get/devices/name")
			if err != nil {
				return MasterDevicesOutput{}, err
			}
			classRes, err := client.Query("/live/master/get/devices/class_name")
			if err != nil {
				return MasterDevicesOutput{}, err
			}
			typeRes, err := client.Query("/live/master/get/devices/type")
			if err != nil {
				return MasterDevicesOutput{}, err
			}
			n := len(namesRes)
			if len(classRes) < n {
				n = len(classRes)
			}
			if len(typeRes) < n {
				n = len(typeRes)
			}
			devices := make([]TrackDevice, 0, n)
			for i := 0; i < n; i++ {
				t, err := abletonosc.AsInt(typeRes[i])
				if err != nil {
					return MasterDevicesOutput{}, err
				}
				devices = append(devices, TrackDevice{
					Name:      fmt.Sprint(namesRes[i]),
					ClassName: fmt.Sprint(classRes[i]),
					Type:      t,
				})
			}
			return MasterDevicesOutput{Devices: devices}, nil
		},
	)
}

func NewAbletonGetMasterDeviceParameters(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_master_device_parameters", "Ableton Live: get master device parameters (requires master patch)",
		func(_ *ai.ToolContext, input MasterDeviceParametersInput) (DeviceParametersOutput, error) {
			if input.DeviceIndex < 0 {
				return DeviceParametersOutput{}, errors.New("device_index must be >= 0")
			}
			idx := int32(input.DeviceIndex)
			namesRes, err := client.Query("/live/master/device/get/parameters/name", idx)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			valuesRes, err := client.Query("/live/master/device/get/parameters/value", idx)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			minsRes, err := client.Query("/live/master/device/get/parameters/min", idx)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			maxsRes, err := client.Query("/live/master/device/get/parameters/max", idx)
			if err != nil {
				return DeviceParametersOutput{}, err
			}
			names := skipLeadingIndex(namesRes)
			values := skipLeadingIndex(valuesRes)
			mins := skipLeadingIndex(minsRes)
			maxs := skipLeadingIndex(maxsRes)
			n := len(names)
			if len(values) < n {
				n = len(values)
			}
			if len(mins) < n {
				n = len(mins)
			}
			if len(maxs) < n {
				n = len(maxs)
			}
			params := make([]DeviceParameter, 0, n)
			for i := 0; i < n; i++ {
				v, err := abletonosc.AsFloat64(values[i])
				if err != nil {
					return DeviceParametersOutput{}, err
				}
				mn, err := abletonosc.AsFloat64(mins[i])
				if err != nil {
					return DeviceParametersOutput{}, err
				}
				mx, err := abletonosc.AsFloat64(maxs[i])
				if err != nil {
					return DeviceParametersOutput{}, err
				}
				params = append(params, DeviceParameter{
					Index: i,
					Name:  fmt.Sprint(names[i]),
					Value: v,
					Min:   mn,
					Max:   mx,
				})
			}
			return DeviceParametersOutput{
				TrackIndex:  -1,
				DeviceIndex: input.DeviceIndex,
				Parameters:  params,
			}, nil
		},
	)
}

func NewAbletonSetMasterDeviceParameter(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_master_device_parameter", "Ableton Live: set a master device parameter (requires master patch)",
		func(_ *ai.ToolContext, input SetMasterDeviceParameterInput) (SentOutput, error) {
			if input.DeviceIndex < 0 || input.ParameterIndex < 0 {
				return SentOutput{}, errors.New("device_index and parameter_index must be >= 0")
			}
			if _, err := client.Query(
				"/live/master/device/set/parameter/value",
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

func NewAbletonLoadOnMaster(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_load_on_master", "Ableton Live: load a Browser item onto the master track (requires browser+master patch)",
		func(_ *ai.ToolContext, input LoadOnMasterInput) (LoadOnMasterOutput, error) {
			if input.RootName == "" || input.ItemName == "" {
				return LoadOnMasterOutput{}, errors.New("root_name and item_name are required")
			}
			args := []interface{}{int32(-1), int32(-1), input.RootName}
			for _, p := range input.PathParts {
				args = append(args, p)
			}
			args = append(args, input.ItemName)
			res, err := client.QueryWithTimeout(10*time.Second, "/live/browser/load_at_path", args...)
			if err != nil {
				return LoadOnMasterOutput{}, err
			}
			if err := ensureResponseLen(res, 5); err != nil {
				return LoadOnMasterOutput{}, err
			}
			status := fmt.Sprint(res[1])
			if status != "loaded" {
				return LoadOnMasterOutput{}, fmt.Errorf("load failed: status=%s reply=%v", status, res)
			}
			before, err := abletonosc.AsInt(res[3])
			if err != nil {
				return LoadOnMasterOutput{}, err
			}
			after, err := abletonosc.AsInt(res[4])
			if err != nil {
				return LoadOnMasterOutput{}, err
			}
			return LoadOnMasterOutput{
				Loaded:        fmt.Sprint(res[2]),
				DevicesBefore: before,
				DevicesAfter:  after,
			}, nil
		},
	)
}

func queryMeter(client *abletonosc.Client, address string, args ...interface{}) (float64, error) {
	res, err := client.Query(address, args...)
	if err != nil {
		return 0, err
	}
	// track meter: [track_index, value]; master meter: [value]
	if len(res) >= 2 {
		return abletonosc.AsFloat64(res[1])
	}
	if len(res) >= 1 {
		return abletonosc.AsFloat64(res[0])
	}
	return 0, errors.New("empty meter response")
}

func skipLeadingIndex(res []interface{}) []interface{} {
	if len(res) == 0 {
		return res
	}
	return res[1:]
}
