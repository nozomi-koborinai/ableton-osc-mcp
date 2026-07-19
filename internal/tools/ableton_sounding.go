package tools

import (
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// SoundingDevice is a device in the track chain (name + class for resume anchors).
type SoundingDevice struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	ClassName string `json:"class_name,omitempty"`
}

// SoundingTrack is one track's mute/solo/playing state, device chain, and clip presence.
type SoundingTrack struct {
	Index             int              `json:"index"`
	Name              string           `json:"name"`
	Mute              bool             `json:"mute"`
	Solo              bool             `json:"solo"`
	PlayingSlotIndex  int              `json:"playing_slot_index" jsonschema:"description=-1 when nothing is playing on this track"`
	Devices           []SoundingDevice `json:"devices"`
	ClipSlotsOccupied []int            `json:"clip_slots_occupied" jsonschema:"description=Scene indices that currently have a clip"`
}

type SoundingSnapshotOutput struct {
	TempoBPM  float64          `json:"tempo_bpm"`
	IsPlaying bool             `json:"is_playing"`
	Scenes    []SceneNameEntry `json:"scenes"`
	Tracks    []SoundingTrack  `json:"tracks"`
}

type soundingQuerier interface {
	Query(address string, args ...interface{}) ([]interface{}, error)
}

// parseTrackDataBlock unpacks a flat /live/song/get/track_data reply for:
// track.name, track.mute, track.solo, track.playing_slot_index, clip_slot.has_clip
// Layout per track: name, mute, solo, playing_slot, then numScenes has_clip bools.
func parseTrackDataBlock(values []interface{}, numTracks, numScenes int) ([]SoundingTrack, error) {
	need := numTracks * (4 + numScenes)
	if len(values) < need {
		return nil, fmt.Errorf("track_data too short: got %d want >= %d (tracks=%d scenes=%d)", len(values), need, numTracks, numScenes)
	}
	tracks := make([]SoundingTrack, 0, numTracks)
	i := 0
	for t := 0; t < numTracks; t++ {
		name := fmt.Sprint(values[i])
		i++
		mute, err := abletonosc.AsBool(values[i])
		if err != nil {
			return nil, fmt.Errorf("track %d mute: %w", t, err)
		}
		i++
		solo, err := abletonosc.AsBool(values[i])
		if err != nil {
			return nil, fmt.Errorf("track %d solo: %w", t, err)
		}
		i++
		playing, err := abletonosc.AsInt(values[i])
		if err != nil {
			return nil, fmt.Errorf("track %d playing_slot: %w", t, err)
		}
		i++
		occupied := make([]int, 0)
		for s := 0; s < numScenes; s++ {
			has, err := abletonosc.AsBool(values[i])
			if err != nil {
				return nil, fmt.Errorf("track %d scene %d has_clip: %w", t, s, err)
			}
			i++
			if has {
				occupied = append(occupied, s)
			}
		}
		tracks = append(tracks, SoundingTrack{
			Index:             t,
			Name:              name,
			Mute:              mute,
			Solo:              solo,
			PlayingSlotIndex:  playing,
			Devices:           []SoundingDevice{},
			ClipSlotsOccupied: occupied,
		})
	}
	return tracks, nil
}

func getSoundingSnapshot(client soundingQuerier) (SoundingSnapshotOutput, error) {
	tempoRes, err := client.Query("/live/song/get/tempo")
	if err != nil {
		return SoundingSnapshotOutput{}, fmt.Errorf("get tempo: %w", err)
	}
	if err := ensureResponseLen(tempoRes, 1); err != nil {
		return SoundingSnapshotOutput{}, err
	}
	tempo, err := abletonosc.AsFloat64(tempoRes[0])
	if err != nil {
		return SoundingSnapshotOutput{}, err
	}

	playingRes, err := client.Query("/live/song/get/is_playing")
	if err != nil {
		return SoundingSnapshotOutput{}, fmt.Errorf("get is_playing: %w", err)
	}
	if err := ensureResponseLen(playingRes, 1); err != nil {
		return SoundingSnapshotOutput{}, err
	}
	isPlaying, err := abletonosc.AsBool(playingRes[0])
	if err != nil {
		return SoundingSnapshotOutput{}, err
	}

	numTracks, err := queryNumTracks(client)
	if err != nil {
		return SoundingSnapshotOutput{}, fmt.Errorf("get num tracks: %w", err)
	}
	numScenes, err := queryNumScenes(client)
	if err != nil {
		return SoundingSnapshotOutput{}, fmt.Errorf("get num scenes: %w", err)
	}

	sceneNames, err := querySceneNames(client)
	if err != nil {
		return SoundingSnapshotOutput{}, fmt.Errorf("get scene names: %w", err)
	}
	scenes := make([]SceneNameEntry, 0, len(sceneNames))
	for i, n := range sceneNames {
		scenes = append(scenes, SceneNameEntry{Index: i, Name: n})
	}

	out := SoundingSnapshotOutput{
		TempoBPM:  tempo,
		IsPlaying: isPlaying,
		Scenes:    scenes,
		Tracks:    []SoundingTrack{},
	}
	if numTracks == 0 {
		return out, nil
	}

	// Bulk: name/mute/solo/playing + has_clip grid. clip_slot count equals scene count in Session view.
	dataRes, err := client.Query(
		"/live/song/get/track_data",
		int32(0), int32(-1),
		"track.name", "track.mute", "track.solo", "track.playing_slot_index", "clip_slot.has_clip",
	)
	if err != nil {
		return SoundingSnapshotOutput{}, fmt.Errorf("get track_data: %w", err)
	}
	tracks, err := parseTrackDataBlock(dataRes, numTracks, numScenes)
	if err != nil {
		return SoundingSnapshotOutput{}, err
	}

	for t := range tracks {
		namesRes, err := client.Query("/live/track/get/devices/name", int32(t))
		if err != nil {
			return SoundingSnapshotOutput{}, fmt.Errorf("get devices name track %d: %w", t, err)
		}
		classRes, err := client.Query("/live/track/get/devices/class_name", int32(t))
		if err != nil {
			return SoundingSnapshotOutput{}, fmt.Errorf("get devices class track %d: %w", t, err)
		}
		if err := ensureResponseLen(namesRes, 1); err != nil {
			return SoundingSnapshotOutput{}, err
		}
		names := toStringSlice(namesRes[1:])
		classes := []string{}
		if len(classRes) > 1 {
			classes = toStringSlice(classRes[1:])
		}
		devs := make([]SoundingDevice, 0, len(names))
		for i, n := range names {
			d := SoundingDevice{Index: i, Name: n}
			if i < len(classes) {
				d.ClassName = classes[i]
			}
			devs = append(devs, d)
		}
		tracks[t].Devices = devs
	}

	out.Tracks = tracks
	return out, nil
}

func NewAbletonGetSoundingSnapshot(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_sounding_snapshot",
		"Ableton Live: conversation-resume anchor — tempo, playback, scene names, and per-track mute/solo/playing slot, device chain, and which scenes currently have clips. Prefer this over ableton_get_session_snapshot when you need to know what is actually set up to sound.",
		func(_ *ai.ToolContext, _ struct{}) (SoundingSnapshotOutput, error) {
			return getSoundingSnapshot(client)
		},
	)
}
