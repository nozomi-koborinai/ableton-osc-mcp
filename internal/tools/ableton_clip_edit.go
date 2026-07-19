package tools

import (
	"errors"
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// clipWarpModes indexes the Live Object Model Clip.warp_mode enum.
var clipWarpModes = []string{"Beats", "Tones", "Texture", "Re-Pitch", "Complex", "REX", "Complex Pro"}

func clipGet(client *abletonosc.Client, track, clip int, prop string) (interface{}, error) {
	res, err := client.Query("/live/clip/get/"+prop, int32(track), int32(clip))
	if err != nil {
		return nil, err
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return nil, err
	}
	return res[2], nil
}

func requireAudioClip(client *abletonosc.Client, track, clip int) error {
	v, err := clipGet(client, track, clip, "is_audio_clip")
	if err != nil {
		return fmt.Errorf("clip not found or slot empty: %w", err)
	}
	isAudio, _ := abletonosc.AsBool(v)
	if !isAudio {
		return errors.New("clip is not an audio clip (pitch/warp apply to audio clips only)")
	}
	return nil
}

func slotHasClip(client *abletonosc.Client, track, clip int) (bool, error) {
	res, err := client.Query("/live/clip_slot/get/has_clip", int32(track), int32(clip))
	if err != nil {
		return false, err
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, err
	}
	return abletonosc.AsBool(res[2])
}

type ClipPropertiesInput struct {
	TrackIndex int `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"minimum=0"`
}

type ClipProperties struct {
	TrackIndex   int      `json:"track_index"`
	ClipIndex    int      `json:"clip_index"`
	IsAudioClip  bool     `json:"is_audio_clip"`
	LengthBeats  float64  `json:"length_beats"`
	FilePath     string   `json:"file_path,omitempty"`
	Gain         *float64 `json:"gain,omitempty"`
	GainDisplay  string   `json:"gain_display,omitempty"`
	PitchCoarse  *int     `json:"pitch_coarse,omitempty" jsonschema:"description=Transpose in semitones (audio clips)"`
	PitchFine    *int     `json:"pitch_fine,omitempty" jsonschema:"description=Detune in cents (audio clips)"`
	WarpMode     *int     `json:"warp_mode,omitempty"`
	WarpModeName string   `json:"warp_mode_name,omitempty"`
	Warping      *bool    `json:"warping,omitempty"`
	StartMarker  *float64 `json:"start_marker,omitempty"`
	EndMarker    *float64 `json:"end_marker,omitempty"`
	LoopStart    *float64 `json:"loop_start,omitempty"`
	LoopEnd      *float64 `json:"loop_end,omitempty"`
}

func NewAbletonGetClipProperties(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_clip_properties",
		"Ableton Live: get a clip's edit state. Audio clips: pitch_coarse (transpose semitones), pitch_fine (detune cents), warp_mode/warping, gain, file_path. Both: length, start/end markers, loop. Uses stock AbletonOSC",
		func(_ *ai.ToolContext, input ClipPropertiesInput) (ClipProperties, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return ClipProperties{}, err
			}
			t, c := input.TrackIndex, input.ClipIndex
			out := ClipProperties{TrackIndex: t, ClipIndex: c}

			v, err := clipGet(client, t, c, "is_audio_clip")
			if err != nil {
				return ClipProperties{}, fmt.Errorf("get clip failed (is the slot empty?): %w", err)
			}
			out.IsAudioClip, _ = abletonosc.AsBool(v)

			if v, err := clipGet(client, t, c, "length"); err == nil {
				out.LengthBeats, _ = abletonosc.AsFloat64(v)
			}
			for _, m := range []struct {
				prop string
				dst  **float64
			}{
				{"start_marker", &out.StartMarker},
				{"end_marker", &out.EndMarker},
				{"loop_start", &out.LoopStart},
				{"loop_end", &out.LoopEnd},
			} {
				if v, err := clipGet(client, t, c, m.prop); err == nil {
					if f, e := abletonosc.AsFloat64(v); e == nil {
						val := f
						*m.dst = &val
					}
				}
			}

			if out.IsAudioClip {
				if v, err := clipGet(client, t, c, "file_path"); err == nil {
					out.FilePath = fmt.Sprint(v)
				}
				if v, err := clipGet(client, t, c, "gain"); err == nil {
					if f, e := abletonosc.AsFloat64(v); e == nil {
						val := f
						out.Gain = &val
					}
				}
				if v, err := clipGet(client, t, c, "gain_display_string"); err == nil {
					out.GainDisplay = fmt.Sprint(v)
				}
				if v, err := clipGet(client, t, c, "pitch_coarse"); err == nil {
					if n, e := abletonosc.AsInt(v); e == nil {
						val := n
						out.PitchCoarse = &val
					}
				}
				if v, err := clipGet(client, t, c, "pitch_fine"); err == nil {
					if n, e := abletonosc.AsInt(v); e == nil {
						val := n
						out.PitchFine = &val
					}
				}
				if v, err := clipGet(client, t, c, "warping"); err == nil {
					if b, e := abletonosc.AsBool(v); e == nil {
						val := b
						out.Warping = &val
					}
				}
				if v, err := clipGet(client, t, c, "warp_mode"); err == nil {
					if n, e := abletonosc.AsInt(v); e == nil {
						val := n
						out.WarpMode = &val
						out.WarpModeName = nameForIndex(clipWarpModes, n)
					}
				}
			}
			return out, nil
		},
	)
}

type SetClipPitchInput struct {
	TrackIndex int  `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int  `json:"clip_index" jsonschema:"minimum=0"`
	Coarse     *int `json:"coarse,omitempty" jsonschema:"description=Transpose in semitones (-48..48)"`
	Fine       *int `json:"fine,omitempty" jsonschema:"description=Detune in cents (-50..50)"`
}

type ClipPitchOutput struct {
	TrackIndex  int `json:"track_index"`
	ClipIndex   int `json:"clip_index"`
	PitchCoarse int `json:"pitch_coarse"`
	PitchFine   int `json:"pitch_fine"`
}

func NewAbletonSetClipPitch(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_clip_pitch",
		"Ableton Live: transpose (coarse semitones) and/or detune (fine cents) an audio clip. Key-matching by sample edit, not project tempo. Uses stock AbletonOSC",
		func(_ *ai.ToolContext, input SetClipPitchInput) (ClipPitchOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return ClipPitchOutput{}, err
			}
			if input.Coarse == nil && input.Fine == nil {
				return ClipPitchOutput{}, errors.New("provide coarse and/or fine")
			}
			if err := requireAudioClip(client, input.TrackIndex, input.ClipIndex); err != nil {
				return ClipPitchOutput{}, err
			}
			if input.Coarse != nil {
				if *input.Coarse < -48 || *input.Coarse > 48 {
					return ClipPitchOutput{}, errors.New("coarse must be -48..48 semitones")
				}
				if err := client.Send("/live/clip/set/pitch_coarse", int32(input.TrackIndex), int32(input.ClipIndex), int32(*input.Coarse)); err != nil {
					return ClipPitchOutput{}, err
				}
			}
			if input.Fine != nil {
				if *input.Fine < -50 || *input.Fine > 50 {
					return ClipPitchOutput{}, errors.New("fine must be -50..50 cents")
				}
				if err := client.Send("/live/clip/set/pitch_fine", int32(input.TrackIndex), int32(input.ClipIndex), int32(*input.Fine)); err != nil {
					return ClipPitchOutput{}, err
				}
			}
			out := ClipPitchOutput{TrackIndex: input.TrackIndex, ClipIndex: input.ClipIndex}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "pitch_coarse"); err == nil {
				out.PitchCoarse, _ = abletonosc.AsInt(v)
			}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "pitch_fine"); err == nil {
				out.PitchFine, _ = abletonosc.AsInt(v)
			}
			return out, nil
		},
	)
}

type SetClipWarpInput struct {
	TrackIndex int    `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int    `json:"clip_index" jsonschema:"minimum=0"`
	Warping    *bool  `json:"warping,omitempty" jsonschema:"description=Enable/disable warping"`
	WarpMode   string `json:"warp_mode,omitempty" jsonschema:"description=Beats, Tones, Texture, Re-Pitch, Complex, REX, or Complex Pro"`
}

type ClipWarpOutput struct {
	TrackIndex   int    `json:"track_index"`
	ClipIndex    int    `json:"clip_index"`
	Warping      bool   `json:"warping"`
	WarpMode     int    `json:"warp_mode"`
	WarpModeName string `json:"warp_mode_name"`
}

func NewAbletonSetClipWarp(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_clip_warp",
		"Ableton Live: set an audio clip's warping on/off and/or warp mode (Beats/Tones/Texture/Re-Pitch/Complex/REX/Complex Pro). Uses stock AbletonOSC",
		func(_ *ai.ToolContext, input SetClipWarpInput) (ClipWarpOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return ClipWarpOutput{}, err
			}
			if input.Warping == nil && input.WarpMode == "" {
				return ClipWarpOutput{}, errors.New("provide warping and/or warp_mode")
			}
			if err := requireAudioClip(client, input.TrackIndex, input.ClipIndex); err != nil {
				return ClipWarpOutput{}, err
			}
			if input.Warping != nil {
				warpVal := int32(0)
				if *input.Warping {
					warpVal = 1
				}
				if err := client.Send("/live/clip/set/warping", int32(input.TrackIndex), int32(input.ClipIndex), warpVal); err != nil {
					return ClipWarpOutput{}, err
				}
			}
			if input.WarpMode != "" {
				idx, ok := indexForName(clipWarpModes, input.WarpMode)
				if !ok {
					return ClipWarpOutput{}, fmt.Errorf("invalid warp_mode %q; use one of: Beats, Tones, Texture, Re-Pitch, Complex, REX, Complex Pro", input.WarpMode)
				}
				if err := client.Send("/live/clip/set/warp_mode", int32(input.TrackIndex), int32(input.ClipIndex), int32(idx)); err != nil {
					return ClipWarpOutput{}, err
				}
			}
			out := ClipWarpOutput{TrackIndex: input.TrackIndex, ClipIndex: input.ClipIndex}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "warping"); err == nil {
				out.Warping, _ = abletonosc.AsBool(v)
			}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "warp_mode"); err == nil {
				out.WarpMode, _ = abletonosc.AsInt(v)
				out.WarpModeName = nameForIndex(clipWarpModes, out.WarpMode)
			}
			return out, nil
		},
	)
}

type SetClipRegionInput struct {
	TrackIndex  int      `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex   int      `json:"clip_index" jsonschema:"minimum=0"`
	StartMarker *float64 `json:"start_marker,omitempty" jsonschema:"description=Start marker in beats"`
	EndMarker   *float64 `json:"end_marker,omitempty" jsonschema:"description=End marker in beats"`
	LoopStart   *float64 `json:"loop_start,omitempty" jsonschema:"description=Loop start in beats"`
	LoopEnd     *float64 `json:"loop_end,omitempty" jsonschema:"description=Loop end in beats"`
}

type ClipRegionOutput struct {
	TrackIndex  int     `json:"track_index"`
	ClipIndex   int     `json:"clip_index"`
	StartMarker float64 `json:"start_marker"`
	EndMarker   float64 `json:"end_marker"`
	LoopStart   float64 `json:"loop_start"`
	LoopEnd     float64 `json:"loop_end"`
}

func NewAbletonSetClipRegion(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_clip_region",
		"Ableton Live: set a clip's start/end markers and loop points (in beats). Non-destructive way to focus a region before cropping. Uses stock AbletonOSC",
		func(_ *ai.ToolContext, input SetClipRegionInput) (ClipRegionOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return ClipRegionOutput{}, err
			}
			if input.StartMarker == nil && input.EndMarker == nil && input.LoopStart == nil && input.LoopEnd == nil {
				return ClipRegionOutput{}, errors.New("provide at least one of start_marker, end_marker, loop_start, loop_end")
			}
			for _, m := range []struct {
				prop string
				val  *float64
			}{
				{"start_marker", input.StartMarker},
				{"end_marker", input.EndMarker},
				{"loop_start", input.LoopStart},
				{"loop_end", input.LoopEnd},
			} {
				if m.val == nil {
					continue
				}
				if *m.val < 0 {
					return ClipRegionOutput{}, fmt.Errorf("%s must be >= 0", m.prop)
				}
				if err := client.Send("/live/clip/set/"+m.prop, int32(input.TrackIndex), int32(input.ClipIndex), float32(*m.val)); err != nil {
					return ClipRegionOutput{}, err
				}
			}
			out := ClipRegionOutput{TrackIndex: input.TrackIndex, ClipIndex: input.ClipIndex}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "start_marker"); err == nil {
				out.StartMarker, _ = abletonosc.AsFloat64(v)
			}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "end_marker"); err == nil {
				out.EndMarker, _ = abletonosc.AsFloat64(v)
			}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "loop_start"); err == nil {
				out.LoopStart, _ = abletonosc.AsFloat64(v)
			}
			if v, err := clipGet(client, input.TrackIndex, input.ClipIndex, "loop_end"); err == nil {
				out.LoopEnd, _ = abletonosc.AsFloat64(v)
			}
			return out, nil
		},
	)
}

type ExtractClipRegionInput struct {
	TrackIndex      int     `json:"track_index" jsonschema:"minimum=0"`
	SourceClipIndex int     `json:"source_clip_index" jsonschema:"description=Slot holding the source audio clip,minimum=0"`
	TargetClipIndex int     `json:"target_clip_index" jsonschema:"description=Empty slot to place the extracted region,minimum=0"`
	StartBeats      float64 `json:"start_beats" jsonschema:"description=Region start within the source clip (beats)"`
	EndBeats        float64 `json:"end_beats" jsonschema:"description=Region end within the source clip (beats)"`
	Name            string  `json:"name,omitempty" jsonschema:"description=Optional name for the new clip"`
	Loop            *bool   `json:"loop,omitempty" jsonschema:"description=Loop the extracted region (default true)"`
}

type ExtractClipRegionOutput struct {
	TrackIndex  int     `json:"track_index"`
	ClipIndex   int     `json:"clip_index"`
	StartMarker float64 `json:"start_marker"`
	EndMarker   float64 `json:"end_marker"`
	LoopStart   float64 `json:"loop_start"`
	LoopEnd     float64 `json:"loop_end"`
	LengthBeats float64 `json:"length_beats"`
	Name        string  `json:"name,omitempty"`
}

func NewAbletonExtractClipRegion(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_extract_clip_region",
		"Ableton Live: copy a region [start_beats, end_beats] of an existing audio clip into an empty slot as a new clip (non-destructive chop). Duplicates the source, then focuses markers/loop to the region. Turns one long sample into per-hit clips. Uses stock AbletonOSC",
		func(_ *ai.ToolContext, input ExtractClipRegionInput) (ExtractClipRegionOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.SourceClipIndex); err != nil {
				return ExtractClipRegionOutput{}, err
			}
			if input.TargetClipIndex < 0 {
				return ExtractClipRegionOutput{}, errors.New("target_clip_index must be >= 0")
			}
			if input.TargetClipIndex == input.SourceClipIndex {
				return ExtractClipRegionOutput{}, errors.New("target_clip_index must differ from source_clip_index")
			}
			if input.StartBeats < 0 {
				return ExtractClipRegionOutput{}, errors.New("start_beats must be >= 0")
			}
			if input.EndBeats <= input.StartBeats {
				return ExtractClipRegionOutput{}, errors.New("end_beats must be > start_beats")
			}
			if err := requireAudioClip(client, input.TrackIndex, input.SourceClipIndex); err != nil {
				return ExtractClipRegionOutput{}, err
			}
			if v, err := clipGet(client, input.TrackIndex, input.SourceClipIndex, "length"); err == nil {
				if srcLen, e := abletonosc.AsFloat64(v); e == nil && input.EndBeats > srcLen+1e-6 {
					return ExtractClipRegionOutput{}, fmt.Errorf("end_beats %.3f exceeds source clip length %.3f beats", input.EndBeats, srcLen)
				}
			}
			if has, err := slotHasClip(client, input.TrackIndex, input.TargetClipIndex); err != nil {
				return ExtractClipRegionOutput{}, fmt.Errorf("check target slot: %w", err)
			} else if has {
				return ExtractClipRegionOutput{}, fmt.Errorf("target slot %d is not empty; choose an empty slot", input.TargetClipIndex)
			}
			// AbletonOSC expects (src_track, src_clip, target_track, target_clip).
			if err := client.Send("/live/clip_slot/duplicate_clip_to",
				int32(input.TrackIndex), int32(input.SourceClipIndex),
				int32(input.TrackIndex), int32(input.TargetClipIndex)); err != nil {
				return ExtractClipRegionOutput{}, err
			}
			has, err := slotHasClip(client, input.TrackIndex, input.TargetClipIndex)
			if err != nil {
				return ExtractClipRegionOutput{}, err
			}
			if !has {
				return ExtractClipRegionOutput{}, errors.New("duplicate did not populate the target slot")
			}

			loopOn := true
			if input.Loop != nil {
				loopOn = *input.Loop
			}
			t, c := input.TrackIndex, input.TargetClipIndex
			setClip := func(prop string, val interface{}) error {
				return client.Send("/live/clip/set/"+prop, int32(t), int32(c), val)
			}
			loopVal := int32(0)
			if loopOn {
				loopVal = 1
			}
			// Focus the duplicated (full-length) clip down to the region. Order is
			// safe because the region is a sub-range of the existing [0, length].
			if err := setClip("looping", loopVal); err != nil {
				return ExtractClipRegionOutput{}, err
			}
			if err := setClip("start_marker", float32(input.StartBeats)); err != nil {
				return ExtractClipRegionOutput{}, err
			}
			if err := setClip("end_marker", float32(input.EndBeats)); err != nil {
				return ExtractClipRegionOutput{}, err
			}
			if loopOn {
				if err := setClip("loop_start", float32(input.StartBeats)); err != nil {
					return ExtractClipRegionOutput{}, err
				}
				if err := setClip("loop_end", float32(input.EndBeats)); err != nil {
					return ExtractClipRegionOutput{}, err
				}
			}
			if input.Name != "" {
				if err := setClip("name", input.Name); err != nil {
					return ExtractClipRegionOutput{}, err
				}
			}

			out := ExtractClipRegionOutput{TrackIndex: t, ClipIndex: c, Name: input.Name}
			if v, err := clipGet(client, t, c, "start_marker"); err == nil {
				out.StartMarker, _ = abletonosc.AsFloat64(v)
			}
			if v, err := clipGet(client, t, c, "end_marker"); err == nil {
				out.EndMarker, _ = abletonosc.AsFloat64(v)
			}
			if v, err := clipGet(client, t, c, "loop_start"); err == nil {
				out.LoopStart, _ = abletonosc.AsFloat64(v)
			}
			if v, err := clipGet(client, t, c, "loop_end"); err == nil {
				out.LoopEnd, _ = abletonosc.AsFloat64(v)
			}
			if v, err := clipGet(client, t, c, "length"); err == nil {
				out.LengthBeats, _ = abletonosc.AsFloat64(v)
			}
			return out, nil
		},
	)
}
