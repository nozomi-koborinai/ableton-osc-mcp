package tools

import "sort"

// deviceOnParameter is the index of "Device On": the first parameter of every
// Live device, and the only way to switch one on or off from outside.
const deviceOnParameter = 0

// auditionState holds the value of everything any variant touches. A clip
// value of -1 means nothing is playing on that track.
type auditionState struct {
	clips   map[int]int
	volumes map[int]float64
	devices map[[2]int]bool
}

// auditionVariantState is one variant with its mix deltas already resolved to
// raw fader positions. It names only what the variant itself changes.
type auditionVariantState struct {
	Label       string
	Description string
	clips       map[int]int
	volumes     map[int]float64
	devices     map[[2]int]bool
}

// auditionCommand is one OSC message. Quantized commands (clip fires and stops)
// take effect on the next bar line and are sent ahead of it; the rest take
// effect at once and are sent on the line.
type auditionCommand struct {
	address   string
	args      []interface{}
	quantized bool
}

// stateFor is the baseline with the variant's own settings laid over it. Every
// variant is built from the baseline, never from the variant before it, so
// nothing one variant changes can leak into the next.
func stateFor(baseline auditionState, v auditionVariantState) auditionState {
	out := auditionState{
		clips:   make(map[int]int, len(baseline.clips)),
		volumes: make(map[int]float64, len(baseline.volumes)),
		devices: make(map[[2]int]bool, len(baseline.devices)),
	}
	for k, val := range baseline.clips {
		out.clips[k] = val
	}
	for k, val := range baseline.volumes {
		out.volumes[k] = val
	}
	for k, val := range baseline.devices {
		out.devices[k] = val
	}
	for k, val := range v.clips {
		out.clips[k] = val
	}
	for k, val := range v.volumes {
		out.volumes[k] = val
	}
	for k, val := range v.devices {
		out.devices[k] = val
	}
	return out
}

// commandsBetween lists what has to be sent to get from one state to another,
// in a stable order, and nothing for what is already right.
func commandsBetween(from, to auditionState) []auditionCommand {
	var cmds []auditionCommand

	tracks := make([]int, 0, len(to.clips))
	for track := range to.clips {
		tracks = append(tracks, track)
	}
	sort.Ints(tracks)
	for _, track := range tracks {
		slot := to.clips[track]
		if from.clips[track] == slot {
			continue
		}
		if slot < 0 {
			cmds = append(cmds, auditionCommand{"/live/track/stop_all_clips", []interface{}{int32(track)}, true})
		} else {
			cmds = append(cmds, auditionCommand{"/live/clip_slot/fire", []interface{}{int32(track), int32(slot)}, true})
		}
	}

	tracks = tracks[:0]
	for track := range to.volumes {
		tracks = append(tracks, track)
	}
	sort.Ints(tracks)
	for _, track := range tracks {
		if from.volumes[track] != to.volumes[track] {
			cmds = append(cmds, auditionCommand{"/live/track/set/volume", []interface{}{int32(track), float32(to.volumes[track])}, false})
		}
	}

	keys := make([][2]int, 0, len(to.devices))
	for key := range to.devices {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	for _, key := range keys {
		if from.devices[key] != to.devices[key] {
			// A device is switched with its first parameter, "Device On". Live's
			// Device.is_active only reports: writing to it fails.
			cmds = append(cmds, auditionCommand{"/live/device/set/parameter/value", []interface{}{int32(key[0]), int32(key[1]), int32(deviceOnParameter), float32(boolInt32(to.devices[key]))}, false})
		}
	}
	return cmds
}
