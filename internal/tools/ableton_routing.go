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

// --- Return tracks ---

type ReturnTrack struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

type GetReturnTracksOutput struct {
	Returns []ReturnTrack `json:"returns"`
}

func parseReturnTracks(res []interface{}) (GetReturnTracksOutput, error) {
	if err := ensureResponseLen(res, 1); err != nil {
		return GetReturnTracksOutput{}, err
	}
	n, err := abletonosc.AsInt(res[0])
	if err != nil {
		return GetReturnTracksOutput{}, fmt.Errorf("parse count: %w", err)
	}
	names := toStringSlice(res[1:])
	if n != len(names) {
		// Prefer the names we got if Live's count mismatches for any reason.
		n = len(names)
	}
	out := GetReturnTracksOutput{Returns: make([]ReturnTrack, 0, n)}
	for i := 0; i < n; i++ {
		name := ""
		if i < len(names) {
			name = names[i]
		}
		out.Returns = append(out.Returns, ReturnTrack{Index: i, Name: name})
	}
	return out, nil
}

func NewAbletonGetReturnTracks(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_return_tracks",
		"Ableton Live: list return tracks (A/B/C…) with indices used as send_index. Requires the browser patch.",
		func(_ *ai.ToolContext, _ struct{}) (GetReturnTracksOutput, error) {
			res, err := client.Query("/live/song/get/return_tracks")
			if err != nil {
				return GetReturnTracksOutput{}, wrapActionable(err, "return_tracks_unavailable",
					"Install/update the browser patch and restart (or hot-reload) AbletonOSC, then retry.")
			}
			return parseReturnTracks(res)
		},
	)
}

type CreateReturnTrackOutput struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	ReturnsAfter int    `json:"returns_after"`
}

func NewAbletonCreateReturnTrack(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_return_track",
		"Ableton Live: create a new return track at the end of the return rack. Prefer ableton_get_return_tracks afterward for the name (A/B/…). Requires AbletonOSC.",
		func(_ *ai.ToolContext, _ struct{}) (CreateReturnTrackOutput, error) {
			before := 0
			if res, err := client.Query("/live/song/get/return_tracks"); err == nil {
				if parsed, perr := parseReturnTracks(res); perr == nil {
					before = len(parsed.Returns)
				}
			}
			if err := client.Send("/live/song/create_return_track"); err != nil {
				return CreateReturnTrackOutput{}, fmt.Errorf("create return track: %w", err)
			}
			res, err := client.Query("/live/song/get/return_tracks")
			if err != nil {
				// Stock create may have worked even without the list patch.
				return CreateReturnTrackOutput{Index: before, ReturnsAfter: before + 1}, nil
			}
			parsed, err := parseReturnTracks(res)
			if err != nil {
				return CreateReturnTrackOutput{}, err
			}
			after := len(parsed.Returns)
			if after != before+1 && before > 0 {
				return CreateReturnTrackOutput{}, fmt.Errorf("create return track count mismatch (before=%d after=%d)", before, after)
			}
			idx := after - 1
			if idx < 0 {
				return CreateReturnTrackOutput{}, errors.New("no return tracks after create")
			}
			return CreateReturnTrackOutput{
				Index:        idx,
				Name:         parsed.Returns[idx].Name,
				ReturnsAfter: after,
			}, nil
		},
	)
}

// --- Sends ---

type TrackSend struct {
	SendIndex  int     `json:"send_index"`
	ReturnName string  `json:"return_name,omitempty"`
	Value      float64 `json:"value" jsonschema:"description=Normalized send amount (~0..1; ~0.85 ≈ 0 dB)"`
}

type GetTrackSendsInput struct {
	TrackIndex int `json:"track_index" jsonschema:"minimum=0"`
}

type GetTrackSendsOutput struct {
	TrackIndex int         `json:"track_index"`
	Sends      []TrackSend `json:"sends"`
}

func NewAbletonGetTrackSends(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_track_sends",
		"Ableton Live: get all send amounts on a track. send_index matches return track order (0=A, 1=B, …). Values are normalized (~0..1).",
		func(_ *ai.ToolContext, input GetTrackSendsInput) (GetTrackSendsOutput, error) {
			if input.TrackIndex < 0 {
				return GetTrackSendsOutput{}, errors.New("track_index must be >= 0")
			}
			returns := []ReturnTrack{}
			if res, err := client.Query("/live/song/get/return_tracks"); err == nil {
				if parsed, perr := parseReturnTracks(res); perr == nil {
					returns = parsed.Returns
				}
			}
			n := len(returns)
			if n == 0 {
				// Fall back: probe send 0; if it fails, no returns.
				if _, err := client.Query("/live/track/get/send", int32(input.TrackIndex), int32(0)); err != nil {
					return GetTrackSendsOutput{TrackIndex: input.TrackIndex, Sends: []TrackSend{}}, nil
				}
				n = 1
			}
			sends := make([]TrackSend, 0, n)
			for i := 0; i < n; i++ {
				res, err := client.Query("/live/track/get/send", int32(input.TrackIndex), int32(i))
				if err != nil {
					break
				}
				if err := ensureResponseLen(res, 3); err != nil {
					return GetTrackSendsOutput{}, err
				}
				val, err := abletonosc.AsFloat64(res[2])
				if err != nil {
					return GetTrackSendsOutput{}, err
				}
				s := TrackSend{SendIndex: i, Value: val}
				if i < len(returns) {
					s.ReturnName = returns[i].Name
				}
				sends = append(sends, s)
			}
			return GetTrackSendsOutput{TrackIndex: input.TrackIndex, Sends: sends}, nil
		},
	)
}

type SetTrackSendInput struct {
	TrackIndex int     `json:"track_index" jsonschema:"minimum=0"`
	SendIndex  int     `json:"send_index" jsonschema:"description=Return index (0=A, 1=B, …),minimum=0"`
	Value      float64 `json:"value" jsonschema:"description=Normalized send amount (~0..1; ~0.85 ≈ 0 dB)"`
}

type SetTrackSendOutput struct {
	TrackIndex int     `json:"track_index"`
	SendIndex  int     `json:"send_index"`
	Value      float64 `json:"value"`
}

func NewAbletonSetTrackSend(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_track_send",
		"Ableton Live: set a track's send amount to a return (send_index 0=A, 1=B, …). Value is normalized (~0..1; ~0.85 ≈ 0 dB). Confirms by reading back.",
		func(_ *ai.ToolContext, input SetTrackSendInput) (SetTrackSendOutput, error) {
			if input.TrackIndex < 0 || input.SendIndex < 0 {
				return SetTrackSendOutput{}, errors.New("track_index and send_index must be >= 0")
			}
			if err := client.Send("/live/track/set/send",
				int32(input.TrackIndex), int32(input.SendIndex), float32(input.Value),
			); err != nil {
				return SetTrackSendOutput{}, err
			}
			res, err := client.Query("/live/track/get/send", int32(input.TrackIndex), int32(input.SendIndex))
			if err != nil {
				return SetTrackSendOutput(input), nil
			}
			if err := ensureResponseLen(res, 3); err != nil {
				return SetTrackSendOutput{}, err
			}
			val, err := abletonosc.AsFloat64(res[2])
			if err != nil {
				return SetTrackSendOutput{}, err
			}
			return SetTrackSendOutput{
				TrackIndex: input.TrackIndex,
				SendIndex:  input.SendIndex,
				Value:      val,
			}, nil
		},
	)
}

// --- Device sidechain (Compressor input routing) ---

type DeviceSidechainInput struct {
	TrackIndex  int `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int `json:"device_index" jsonschema:"description=Usually a Compressor (or other device with Sidechain input routing),minimum=0"`
}

type DeviceSidechainOutput struct {
	TrackIndex        int      `json:"track_index"`
	DeviceIndex       int      `json:"device_index"`
	RoutingType       string   `json:"routing_type"`
	RoutingChannel    string   `json:"routing_channel,omitempty"`
	AvailableTypes    []string `json:"available_types"`
	AvailableChannels []string `json:"available_channels,omitempty"`
}

func parseDeviceRoutingList(res []interface{}) (track, device int, names []string, err error) {
	if err = ensureResponseLen(res, 3); err != nil {
		return
	}
	track, err = abletonosc.AsInt(res[0])
	if err != nil {
		return
	}
	device, err = abletonosc.AsInt(res[1])
	if err != nil {
		return
	}
	status := fmt.Sprint(res[2])
	switch status {
	case "ok":
		names = toStringSlice(res[3:])
		return
	case "unsupported":
		err = actionable("unsupported_device",
			"device has no sidechain/input routing (need Compressor or similar)",
			"Load Ableton Compressor on the track, enable Sidechain, then retry.")
		return
	case "invalid_track_index", "invalid_device_index":
		err = actionable(status, status,
			"Call ableton_get_track_devices and pick a valid Compressor device_index.")
		return
	default:
		err = fmt.Errorf("device routing list failed: status=%s reply=%v", status, res)
		return
	}
}

func parseDeviceRoutingValue(res []interface{}) (track, device int, value string, err error) {
	if err = ensureResponseLen(res, 3); err != nil {
		return
	}
	track, err = abletonosc.AsInt(res[0])
	if err != nil {
		return
	}
	device, err = abletonosc.AsInt(res[1])
	if err != nil {
		return
	}
	status := fmt.Sprint(res[2])
	switch status {
	case "ok":
		if len(res) < 4 {
			err = errors.New("missing routing value")
			return
		}
		value = fmt.Sprint(res[3])
		return
	case "unsupported":
		err = actionable("unsupported_device",
			"device has no sidechain/input routing",
			"Load Ableton Compressor on the track, enable Sidechain, then retry.")
		return
	default:
		err = fmt.Errorf("device routing get failed: status=%s", status)
		return
	}
}

func parseDeviceRoutingSet(res []interface{}) (string, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return "", err
	}
	status := fmt.Sprint(res[2])
	switch status {
	case "set":
		if len(res) >= 4 {
			return fmt.Sprint(res[3]), nil
		}
		return "", nil
	case "not_found":
		target := ""
		if len(res) > 3 {
			target = fmt.Sprint(res[3])
		}
		opts := toStringSlice(res[4:])
		return "", actionable("routing_not_found",
			fmt.Sprintf("no routing matched %q", target),
			fmt.Sprintf("Pick one of: %s", strings.Join(opts, ", ")))
	case "unsupported":
		return "", actionable("unsupported_device",
			"device has no sidechain/input routing",
			"Load Ableton Compressor on the track, enable Sidechain, then retry.")
	default:
		return "", fmt.Errorf("device routing set failed: status=%s reply=%v", status, res)
	}
}

func NewAbletonGetDeviceSidechain(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_device_sidechain",
		"Ableton Live: get a device's sidechain input routing (Compressor on Live 11+) — current type/channel and available options. Requires the browser patch.",
		func(_ *ai.ToolContext, input DeviceSidechainInput) (DeviceSidechainOutput, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return DeviceSidechainOutput{}, errors.New("track_index and device_index must be >= 0")
			}
			args := []interface{}{int32(input.TrackIndex), int32(input.DeviceIndex)}
			typesRes, err := client.Query("/live/device/get/available_input_routing_types", args...)
			if err != nil {
				return DeviceSidechainOutput{}, wrapActionable(err, "sidechain_unavailable",
					"Install/update the browser patch and restart (or hot-reload) AbletonOSC, then retry.")
			}
			_, _, types, err := parseDeviceRoutingList(typesRes)
			if err != nil {
				return DeviceSidechainOutput{}, err
			}
			typeRes, err := client.Query("/live/device/get/input_routing_type", args...)
			if err != nil {
				return DeviceSidechainOutput{}, err
			}
			_, _, curType, err := parseDeviceRoutingValue(typeRes)
			if err != nil {
				return DeviceSidechainOutput{}, err
			}
			out := DeviceSidechainOutput{
				TrackIndex:     input.TrackIndex,
				DeviceIndex:    input.DeviceIndex,
				RoutingType:    curType,
				AvailableTypes: types,
			}
			if chList, err := client.Query("/live/device/get/available_input_routing_channels", args...); err == nil {
				if _, _, chans, perr := parseDeviceRoutingList(chList); perr == nil {
					out.AvailableChannels = chans
				}
			}
			if chRes, err := client.Query("/live/device/get/input_routing_channel", args...); err == nil {
				if _, _, ch, perr := parseDeviceRoutingValue(chRes); perr == nil {
					out.RoutingChannel = ch
				}
			}
			return out, nil
		},
	)
}

type SetDeviceSidechainInput struct {
	TrackIndex     int    `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex    int    `json:"device_index" jsonschema:"minimum=0"`
	RoutingType    string `json:"routing_type,omitempty" jsonschema:"description=Sidechain source track name (from available_types), e.g. the kick track"`
	RoutingChannel string `json:"routing_channel,omitempty" jsonschema:"description=Optional channel within the source (from available_channels)"`
}

type SetDeviceSidechainOutput struct {
	TrackIndex     int    `json:"track_index"`
	DeviceIndex    int    `json:"device_index"`
	RoutingType    string `json:"routing_type,omitempty"`
	RoutingChannel string `json:"routing_channel,omitempty"`
}

func NewAbletonSetDeviceSidechain(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_device_sidechain",
		"Ableton Live: set a device's sidechain input routing (Compressor). Pass routing_type (source track display name) and optionally routing_channel. Then enable Sidechain on the device via ableton_set_device_parameter_string if needed. Requires the browser patch.",
		func(_ *ai.ToolContext, input SetDeviceSidechainInput) (SetDeviceSidechainOutput, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return SetDeviceSidechainOutput{}, errors.New("track_index and device_index must be >= 0")
			}
			typeName := strings.TrimSpace(input.RoutingType)
			channelName := strings.TrimSpace(input.RoutingChannel)
			if typeName == "" && channelName == "" {
				return SetDeviceSidechainOutput{}, errors.New("routing_type and/or routing_channel is required")
			}
			out := SetDeviceSidechainOutput{TrackIndex: input.TrackIndex, DeviceIndex: input.DeviceIndex}
			if typeName != "" {
				res, err := client.QueryWithTimeout(3*time.Second, "/live/device/set/input_routing_type",
					int32(input.TrackIndex), int32(input.DeviceIndex), typeName)
				if err != nil {
					return out, wrapActionable(err, "sidechain_unavailable",
						"Install/update the browser patch and restart (or hot-reload) AbletonOSC, then retry.")
				}
				set, err := parseDeviceRoutingSet(res)
				if err != nil {
					return out, err
				}
				out.RoutingType = set
			}
			if channelName != "" {
				res, err := client.QueryWithTimeout(3*time.Second, "/live/device/set/input_routing_channel",
					int32(input.TrackIndex), int32(input.DeviceIndex), channelName)
				if err != nil {
					return out, wrapActionable(err, "sidechain_unavailable",
						"Install/update the browser patch and restart (or hot-reload) AbletonOSC, then retry.")
				}
				set, err := parseDeviceRoutingSet(res)
				if err != nil {
					return out, err
				}
				out.RoutingChannel = set
			}
			return out, nil
		},
	)
}
