package tools

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	// The indicator is an audio track at the end of the set whose name says
	// which variant is sounding. Only these two shapes of name are taken for it,
	// so a listener's own "Audition vox" is never renamed.
	auditionIndicatorName    = "Audition"
	auditionIndicatorPrefix  = "Audition ▶"
	maxAuditionIndicatorDesc = 40

	// auditionSettleSeconds is how long before a bar line faders and device
	// switches go out. They take effect the moment Live gets them, up to a tenth
	// of a second after they are sent. Landing a moment early changes the tail of
	// the old bar; landing late would change the downbeat of the new one.
	auditionSettleSeconds = 0.15
)

func auditionTargetMissing(format string, args ...interface{}) error {
	return actionable("audition_target_missing", fmt.Sprintf(format, args...),
		"Call ableton_get_sounding_snapshot to see the tracks, clips and devices that exist. Variant clips have to be written before they can be auditioned. Nothing in Live was touched.")
}

// checkAuditionTargets makes sure every track, clip and device a variant names
// exists. It only reads.
func checkAuditionTargets(c auditionClient, variants []AuditionVariant) error {
	numTracks, err := queryNumTracks(c)
	if err != nil {
		return fmt.Errorf("get track count: %w", err)
	}
	numScenes := -1
	deviceCounts := map[int]int{}
	for _, v := range variants {
		label := strings.TrimSpace(v.Label)
		for _, clip := range v.Clips {
			if clip.TrackIndex >= numTracks {
				return auditionTargetMissing("variant %s: there is no track %d (the set has %d)", label, clip.TrackIndex, numTracks)
			}
			if numScenes < 0 {
				if numScenes, err = queryNumScenes(c); err != nil {
					return err
				}
			}
			if clip.ClipIndex >= numScenes {
				return auditionTargetMissing("variant %s: there is no clip slot %d (the set has %d scenes)", label, clip.ClipIndex, numScenes)
			}
			has, err := queryBool(c, "/live/clip_slot/get/has_clip", int32(clip.TrackIndex), int32(clip.ClipIndex))
			if err != nil {
				return fmt.Errorf("look for the clip of variant %s: %w", label, err)
			}
			if !has {
				return auditionTargetMissing("variant %s: track %d has no clip in slot %d", label, clip.TrackIndex, clip.ClipIndex)
			}
		}
		for _, m := range v.Mix {
			if m.TrackIndex >= numTracks {
				return auditionTargetMissing("variant %s: there is no track %d (the set has %d)", label, m.TrackIndex, numTracks)
			}
		}
		for _, d := range v.Devices {
			if d.TrackIndex >= numTracks {
				return auditionTargetMissing("variant %s: there is no track %d (the set has %d)", label, d.TrackIndex, numTracks)
			}
			count, known := deviceCounts[d.TrackIndex]
			if !known {
				res, err := c.Query("/live/track/get/num_devices", int32(d.TrackIndex))
				if err != nil {
					return fmt.Errorf("count the devices of track %d: %w", d.TrackIndex, err)
				}
				if err := ensureResponseLen(res, 2); err != nil {
					return fmt.Errorf("count the devices of track %d: %w", d.TrackIndex, err)
				}
				if count, err = abletonosc.AsInt(res[1]); err != nil {
					return fmt.Errorf("count the devices of track %d: %w", d.TrackIndex, err)
				}
				deviceCounts[d.TrackIndex] = count
			}
			if d.DeviceIndex >= count {
				return auditionTargetMissing("variant %s: track %d has no device %d (it has %d)", label, d.TrackIndex, d.DeviceIndex, count)
			}
		}
	}
	return nil
}

// captureAuditionBaseline reads the current value of everything any variant
// touches, and nothing else. The levels come along for resolving dB deltas.
func captureAuditionBaseline(c auditionClient, variants []AuditionVariant) (auditionState, map[int]mixerLevel, error) {
	baseline := auditionState{clips: map[int]int{}, volumes: map[int]float64{}, devices: map[[2]int]bool{}}
	levels := map[int]mixerLevel{}
	for _, v := range variants {
		for _, clip := range v.Clips {
			if _, done := baseline.clips[clip.TrackIndex]; done {
				continue
			}
			res, err := c.Query("/live/track/get/playing_slot_index", int32(clip.TrackIndex))
			if err != nil {
				return baseline, nil, fmt.Errorf("get the playing clip of track %d: %w", clip.TrackIndex, err)
			}
			if err := ensureResponseLen(res, 2); err != nil {
				return baseline, nil, fmt.Errorf("get the playing clip of track %d: %w", clip.TrackIndex, err)
			}
			slot, err := abletonosc.AsInt(res[1])
			if err != nil {
				return baseline, nil, fmt.Errorf("get the playing clip of track %d: %w", clip.TrackIndex, err)
			}
			if slot < 0 {
				slot = -1 // Live has more than one way of saying "no Session clip"
			}
			baseline.clips[clip.TrackIndex] = slot
		}
		for _, m := range v.Mix {
			if _, done := levels[m.TrackIndex]; done {
				continue
			}
			level, err := queryMixerLevel(c, trackVolumeTarget(m.TrackIndex))
			if err != nil {
				return baseline, nil, err
			}
			levels[m.TrackIndex] = level
			baseline.volumes[m.TrackIndex] = level.Raw
		}
		for _, d := range v.Devices {
			key := [2]int{d.TrackIndex, d.DeviceIndex}
			if _, done := baseline.devices[key]; done {
				continue
			}
			active, err := queryDeviceOn(c, d.TrackIndex, d.DeviceIndex)
			if err != nil {
				return baseline, nil, err
			}
			baseline.devices[key] = active
		}
	}
	return baseline, levels, nil
}

// resolveAuditionVariants turns every dB delta into the fader position Live
// itself names for it. A level no fader reaches fails here, before anything moves.
func resolveAuditionVariants(c auditionClient, variants []AuditionVariant, levels map[int]mixerLevel) ([]auditionVariantState, error) {
	out := make([]auditionVariantState, 0, len(variants))
	for _, v := range variants {
		state := auditionVariantState{
			Label:       strings.TrimSpace(v.Label),
			Description: strings.TrimSpace(v.Description),
			clips:       map[int]int{}, volumes: map[int]float64{}, devices: map[[2]int]bool{},
		}
		for _, clip := range v.Clips {
			state.clips[clip.TrackIndex] = clip.ClipIndex
		}
		for _, m := range v.Mix {
			level := levels[m.TrackIndex]
			if m.DeltaDB == 0 {
				continue
			}
			if level.Silent {
				return nil, actionable("delta_from_silence",
					fmt.Sprintf("variant %s: track %d is at -inf dB, so a dB change has nothing to start from", state.Label, m.TrackIndex),
					"Set an absolute level first with ableton_set_track_volume (db), then audition changes from there.")
			}
			raw, err := resolveRawForDB(c, trackVolumeTarget(m.TrackIndex), level.DB+m.DeltaDB)
			if err != nil {
				return nil, err
			}
			state.volumes[m.TrackIndex] = raw
		}
		for _, d := range v.Devices {
			state.devices[[2]int{d.TrackIndex, d.DeviceIndex}] = d.Active
		}
		out = append(out, state)
	}
	return out, nil
}

// restoreCommands lists what puts Live back to the baseline from any of the
// given states. After a failure nobody knows how much of a switch went through:
// Live may be in the state before it, the state after it, or in between.
func restoreCommands(baseline auditionState, states ...auditionState) []auditionCommand {
	var out []auditionCommand
	for _, state := range states {
		for _, cmd := range commandsBetween(state, baseline) {
			known := false
			for _, have := range out {
				if reflect.DeepEqual(have, cmd) {
					known = true
					break
				}
			}
			if !known {
				out = append(out, cmd)
			}
		}
	}
	return out
}

// auditionRun is one audition in progress.
type auditionRun struct {
	client      auditionClient
	sleep       auditionSleeper
	tempo       float64
	beatsPerBar int
	baseline    auditionState
	current     auditionState  // what Live has been switched to
	pending     *auditionState // a switch that may be half done
}

// switchTo makes Live sound like next from the bar line on. Clip launches are
// quantized, so they go out ahead of the line; the rest goes out just before it.
func (r *auditionRun) switchTo(next auditionState, line float64) error {
	cmds := commandsBetween(r.current, next)
	r.pending = &next
	steps := []struct {
		at        float64
		quantized bool
	}{
		{line - barLeadBeats(r.tempo, r.beatsPerBar), true},
		{line - auditionSettleSeconds*r.tempo/60, false},
	}
	for _, step := range steps {
		if err := waitUntilSongTime(r.client, r.sleep, step.at, r.tempo); err != nil {
			return err
		}
		for _, cmd := range cmds {
			if cmd.quantized != step.quantized {
				continue
			}
			if err := r.client.Send(cmd.address, cmd.args...); err != nil {
				return fmt.Errorf("%s: %w", cmd.address, err)
			}
		}
	}
	if err := waitUntilSongTime(r.client, r.sleep, line, r.tempo); err != nil {
		return err
	}
	r.current, r.pending = next, nil
	return nil
}

// putBack restores the baseline at once, from wherever a failure left things.
func (r *auditionRun) putBack() {
	states := []auditionState{r.current}
	if r.pending != nil {
		states = append(states, *r.pending)
	}
	playing, err := queryAuditionIsPlaying(r.client)
	stopped := err == nil && !playing
	launched := false
	for _, cmd := range restoreCommands(r.baseline, states...) {
		_ = r.client.Send(cmd.address, cmd.args...)
		launched = launched || cmd.address == "/live/clip_slot/fire"
	}
	if stopped && launched {
		// Launching a clip starts a stopped transport. Whoever stopped it wants it stopped.
		_ = r.client.Send("/live/song/stop_playing")
	}
	r.current, r.pending = r.baseline, nil
}

func (r *auditionRun) failure(sounding, target string, err error) error {
	if errors.Is(err, errTransportStopped) {
		where := "before the first variant had started"
		if sounding != "" {
			where = "while variant " + sounding + " was playing"
		}
		return actionable("audition_interrupted",
			"playback was stopped "+where+"; faders, devices and clips are back where they were",
			"Ask the listener whether they heard enough to choose. To play it again, call ableton_audition again; it starts playback itself.")
	}
	return actionable("audition_failed",
		fmt.Sprintf("could not switch to %s: %v; faders, devices and clips are back where they were", target, err),
		"Call ableton_diagnose to see whether Live still answers, then run the audition again.")
}

func runAudition(client auditionClient, sleep auditionSleeper, input AuditionInput) (AuditionOutput, error) {
	order, err := validateAuditionShape(input)
	if err != nil {
		return AuditionOutput{}, err
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	if err := checkAuditionTargets(client, input.Variants); err != nil {
		return AuditionOutput{}, err
	}
	baseline, levels, err := captureAuditionBaseline(client, input.Variants)
	if err != nil {
		return AuditionOutput{}, err
	}
	variants, err := resolveAuditionVariants(client, input.Variants, levels)
	if err != nil {
		return AuditionOutput{}, err
	}
	tempo, err := queryAuditionTempo(client)
	if err != nil {
		return AuditionOutput{}, err
	}
	beatsPerBar, err := queryAuditionBeatsPerBar(client)
	if err != nil {
		return AuditionOutput{}, err
	}
	run := &auditionRun{client: client, sleep: sleep, tempo: tempo, beatsPerBar: beatsPerBar, baseline: baseline, current: baseline}
	out := AuditionOutput{Played: []AuditionPlayed{}, BarsPerVariant: order.bars, TempoBPM: tempo}

	if order.commit >= 0 {
		return run.commit(out, input, variants[order.commit])
	}
	return run.play(out, input, order, variants)
}

// onBarGrid sets Live up for switching on bar lines and returns what undoes it.
func (r *auditionRun) onBarGrid() (started bool, undo func(), err error) {
	previous, err := queryClipTriggerQuantization(r.client)
	if err != nil {
		return false, nil, err
	}
	if err := r.client.Send("/live/song/set/clip_trigger_quantization", int32(auditionBarQuantization)); err != nil {
		return false, nil, fmt.Errorf("set clip trigger quantization: %w", err)
	}
	undo = func() { _ = r.client.Send("/live/song/set/clip_trigger_quantization", int32(previous)) }
	if started, err = ensureAuditionPlayback(r.client, false); err != nil {
		undo()
		return false, nil, err
	}
	return started, undo, nil
}

func (r *auditionRun) play(out AuditionOutput, input AuditionInput, order auditionOrder, variants []auditionVariantState) (AuditionOutput, error) {
	indicator, shown, err := ensureAuditionIndicator(r.client, r.sleep)
	if err != nil {
		return AuditionOutput{}, err
	}
	show := func(name string) {
		if name != shown && r.client.Send("/live/track/set/name", int32(indicator), name) == nil {
			shown = name
		}
	}
	started, undoGrid, err := r.onBarGrid()
	if err != nil {
		show(auditionIndicatorName)
		return AuditionOutput{}, err
	}
	out.PlaybackStarted = started
	fail := func(sounding, target string, cause error) (AuditionOutput, error) {
		r.putBack()
		show(auditionIndicatorName)
		undoGrid()
		return AuditionOutput{}, r.failure(sounding, target, cause)
	}

	sounding := ""
	_, line, err := nextSafeBarLine(r.client, r.sleep, r.tempo, r.beatsPerBar)
	if err != nil {
		return fail(sounding, "the first variant", err)
	}
	first := line
	for _, index := range order.play {
		v := variants[index]
		if err := r.switchTo(stateFor(r.baseline, v), line); err != nil {
			return fail(sounding, "variant "+v.Label, err)
		}
		sounding = v.Label
		show(indicatorNameFor(v))
		out.Played = append(out.Played, AuditionPlayed{
			Label: v.Label, Description: v.Description,
			StartBar: int(math.Round(line/float64(r.beatsPerBar))) + 1,
			StartSec: (line - first) * 60 / r.tempo,
		})
		line += float64(order.bars * r.beatsPerBar)
	}
	if err := r.switchTo(r.baseline, line); err != nil {
		return fail(sounding, "the original state", err)
	}
	show(auditionIndicatorName)
	undoGrid()
	if input.StopAfter {
		if err := r.client.Send("/live/song/stop_playing"); err != nil {
			return AuditionOutput{}, fmt.Errorf("stop playback: %w", err)
		}
	}

	out.DurationSec = (line - first) * 60 / r.tempo
	out.Restored = true
	labels := make([]string, 0, len(variants))
	for _, v := range variants {
		labels = append(labels, v.Label)
	}
	out.Prompt = "Ask the listener which variant came closest (" + strings.Join(labels, ", ") + "). " +
		"Until they answer, commit nothing and record no choice. " +
		"If they cannot tell the variants apart, change how they are presented (solo the part, a bigger difference, fewer variants) before making new ones."
	return out, nil
}

// commit writes one variant into the set on the next bar line and leaves it.
func (r *auditionRun) commit(out AuditionOutput, input AuditionInput, v auditionVariantState) (AuditionOutput, error) {
	target := stateFor(r.baseline, v)
	if len(commandsBetween(r.baseline, target)) > 0 {
		started, undoGrid, err := r.onBarGrid()
		if err != nil {
			return AuditionOutput{}, err
		}
		out.PlaybackStarted = started
		_, line, err := nextSafeBarLine(r.client, r.sleep, r.tempo, r.beatsPerBar)
		if err == nil {
			err = r.switchTo(target, line)
		}
		if err != nil {
			r.putBack()
			undoGrid()
			return AuditionOutput{}, r.failure("", "variant "+v.Label, err)
		}
		undoGrid()
		if input.StopAfter {
			if err := r.client.Send("/live/song/stop_playing"); err != nil {
				return AuditionOutput{}, fmt.Errorf("stop playback: %w", err)
			}
		}
	}

	committed := &AuditionCommit{Label: v.Label}
	for _, variant := range input.Variants {
		if strings.TrimSpace(variant.Label) != v.Label {
			continue
		}
		committed.Clips, committed.Devices = variant.Clips, variant.Devices
		for _, m := range variant.Mix {
			level, err := queryMixerLevel(r.client, trackVolumeTarget(m.TrackIndex))
			if err != nil {
				return AuditionOutput{}, err
			}
			committed.Mix = append(committed.Mix, AuditionCommittedMix{TrackIndex: m.TrackIndex, VolumeDB: level.Display})
		}
	}
	out.Committed = committed
	out.Prompt = "Variant " + v.Label + " is the current state now: the next audition's X starts from here. " +
		"If the listener chose it, keep the choice with ableton_record_audition_choice."
	return out, nil
}

func indicatorNameFor(v auditionVariantState) string {
	description := []rune(v.Description)
	if len(description) > maxAuditionIndicatorDesc {
		description = append(description[:maxAuditionIndicatorDesc-1], '…')
	}
	return auditionIndicatorPrefix + " " + v.Label + ": " + string(description)
}

// ensureAuditionIndicator finds the indicator track or adds one at the end of
// the set, where it shifts nobody's track index. It returns the track and the
// name it has now.
func ensureAuditionIndicator(c auditionClient, sleep auditionSleeper) (int, string, error) {
	res, err := c.Query("/live/song/get/track_names")
	if err != nil {
		return 0, "", fmt.Errorf("get track names: %w", err)
	}
	names := toStringSlice(res)
	for i := len(names) - 1; i >= 0; i-- {
		if names[i] == auditionIndicatorName || strings.HasPrefix(names[i], auditionIndicatorPrefix) {
			return i, names[i], nil
		}
	}

	// Live selects a track it has just created. The listener's selection comes
	// back afterwards: the indicator is for reading, not for working on.
	selected, selectedErr := c.Query("/live/view/get/selected_track")
	if err := c.Send("/live/song/create_audio_track", int32(-1)); err != nil {
		return 0, "", fmt.Errorf("create the indicator track: %w", err)
	}
	count := len(names)
	for i := 0; i < 50 && count <= len(names); i++ {
		if count, err = queryNumTracks(c); err != nil {
			return 0, "", fmt.Errorf("create the indicator track: %w", err)
		}
		if count <= len(names) {
			sleep(auditionPollInterval)
		}
	}
	if count <= len(names) {
		return 0, "", errors.New("create the indicator track: Live did not add a track")
	}
	index := count - 1
	if err := c.Send("/live/track/set/name", int32(index), auditionIndicatorName); err != nil {
		return 0, "", fmt.Errorf("name the indicator track: %w", err)
	}
	if selectedErr == nil && len(selected) > 0 {
		if previous, err := abletonosc.AsInt(selected[0]); err == nil {
			_ = c.Send("/live/view/set/selected_track", int32(previous))
		}
	}
	return index, auditionIndicatorName, nil
}

// queryDeviceOn reads a device's own on/off switch, its "Device On" parameter.
func queryDeviceOn(client oscQuerier, trackIndex, deviceIndex int) (bool, error) {
	res, err := client.Query("/live/device/get/parameter/value", int32(trackIndex), int32(deviceIndex), int32(deviceOnParameter))
	if err != nil {
		return false, fmt.Errorf("read the on/off switch of device %d on track %d: %w", deviceIndex, trackIndex, err)
	}
	if err := ensureResponseLen(res, 4); err != nil {
		return false, fmt.Errorf("read the on/off switch of device %d on track %d: %w", deviceIndex, trackIndex, err)
	}
	value, err := abletonosc.AsFloat64(res[3])
	if err != nil {
		return false, fmt.Errorf("read the on/off switch of device %d on track %d: %w", deviceIndex, trackIndex, err)
	}
	return value >= 0.5, nil
}
