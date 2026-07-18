package tools

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/splice"
)

type SpliceLibrarySettings struct {
	ConfiguredPath string
}

type GetSpliceLibraryOutput struct {
	Path   string `json:"path,omitempty"`
	Source string `json:"source"`
	Exists bool   `json:"exists"`
	Hint   string `json:"hint,omitempty"`
	Note   string `json:"note"`
}

type SearchSpliceSamplesInput struct {
	Query      string `json:"query,omitempty" jsonschema:"description=Substring match against sample name or relative path"`
	MaxResults *int   `json:"max_results,omitempty" jsonschema:"description=Max matches to return (default 20, max 50),minimum=1,maximum=50"`
}

type SearchSpliceSamplesOutput struct {
	LibraryPath string          `json:"library_path"`
	Query       string          `json:"query,omitempty"`
	Samples     []splice.Sample `json:"samples"`
	Note        string          `json:"note"`
}

type LoadSpliceSampleInput struct {
	AbsolutePath string `json:"absolute_path,omitempty" jsonschema:"description=Absolute path from ableton_search_splice_samples"`
	RelativePath string `json:"relative_path,omitempty" jsonschema:"description=Path relative to the resolved Splice library root"`
	TrackIndex   int    `json:"track_index" jsonschema:"description=Destination audio track index,minimum=0"`
	ClipIndex    int    `json:"clip_index" jsonschema:"description=Session clip slot index,minimum=0"`
	Fire         bool   `json:"fire,omitempty" jsonschema:"description=Fire the clip after loading"`
}

type LoadSpliceSampleOutput struct {
	TrackIndex   int    `json:"track_index"`
	ClipIndex    int    `json:"clip_index"`
	AbsolutePath string `json:"absolute_path"`
	Fired        bool   `json:"fired"`
	Note         string `json:"note"`
}

type spliceLoadClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonGetSpliceLibrary(g *genkit.Genkit, settings SpliceLibrarySettings) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_splice_library",
		"Locate the local Splice content folder (synced downloads only; does not call the Splice cloud API)",
		func(_ *ai.ToolContext, _ EmptyInput) (GetSpliceLibraryOutput, error) {
			return getSpliceLibrary(settings.ConfiguredPath), nil
		},
	)
}

func NewAbletonSearchSpliceSamples(g *genkit.Genkit, settings SpliceLibrarySettings) ai.Tool {
	return genkit.DefineTool(g, "ableton_search_splice_samples",
		"Search audio files under the local Splice library folder (already downloaded/synced samples only)",
		func(_ *ai.ToolContext, input SearchSpliceSamplesInput) (SearchSpliceSamplesOutput, error) {
			return searchSpliceSamples(settings.ConfiguredPath, input)
		},
	)
}

func NewAbletonLoadSpliceSample(g *genkit.Genkit, client *abletonosc.Client, settings SpliceLibrarySettings) ai.Tool {
	return genkit.DefineTool(g, "ableton_load_splice_sample",
		"Load a local Splice audio file into an empty session clip slot on an audio track (requires Live 12.0.5+ and the AbletonOSC browser patch)",
		func(_ *ai.ToolContext, input LoadSpliceSampleInput) (LoadSpliceSampleOutput, error) {
			return loadSpliceSample(client, settings.ConfiguredPath, input)
		},
	)
}

func getSpliceLibrary(configured string) GetSpliceLibraryOutput {
	lib, err := splice.Resolve(configured)
	if err != nil {
		return GetSpliceLibraryOutput{
			Path:   lib.Path,
			Source: lib.Source,
			Exists: false,
			Hint:   err.Error(),
			Note:   "Cloud Splice search/download is not supported. Sync samples in the Splice desktop app, then point ABLETON_OSC_SPLICE_PATH at that folder if auto-detect fails.",
		}
	}
	return GetSpliceLibraryOutput{
		Path:   lib.Path,
		Source: lib.Source,
		Exists: true,
		Note:   "Local synced Splice library only. Use ableton_search_splice_samples, then ableton_load_splice_sample on an audio track.",
	}
}

func searchSpliceSamples(configured string, input SearchSpliceSamplesInput) (SearchSpliceSamplesOutput, error) {
	lib, err := splice.Resolve(configured)
	if err != nil {
		return SearchSpliceSamplesOutput{}, err
	}
	maxResults := 0
	if input.MaxResults != nil {
		maxResults = *input.MaxResults
	}
	samples, err := splice.Search(lib.Path, input.Query, maxResults)
	if err != nil {
		return SearchSpliceSamplesOutput{}, err
	}
	return SearchSpliceSamplesOutput{
		LibraryPath: lib.Path,
		Query:       strings.TrimSpace(input.Query),
		Samples:     samples,
		Note:        "These files are already on disk. Load one with ableton_load_splice_sample (audio track, Live 12.0.5+).",
	}, nil
}

func loadSpliceSample(client spliceLoadClient, configured string, input LoadSpliceSampleInput) (LoadSpliceSampleOutput, error) {
	if input.TrackIndex < 0 {
		return LoadSpliceSampleOutput{}, errors.New("track_index must be >= 0")
	}
	if input.ClipIndex < 0 {
		return LoadSpliceSampleOutput{}, errors.New("clip_index must be >= 0")
	}
	abs, err := resolveSpliceSamplePath(configured, input.AbsolutePath, input.RelativePath)
	if err != nil {
		return LoadSpliceSampleOutput{}, err
	}

	hasClip, err := querySpliceSlotHasClip(client, input.TrackIndex, input.ClipIndex)
	if err != nil {
		return LoadSpliceSampleOutput{}, fmt.Errorf("check target slot: %w", err)
	}
	if hasClip {
		return LoadSpliceSampleOutput{}, errors.New("clip_index must be an empty slot")
	}

	res, err := client.Query(
		"/live/clip_slot/create_audio_clip",
		int32(input.TrackIndex),
		int32(input.ClipIndex),
		abs,
	)
	if err != nil {
		return LoadSpliceSampleOutput{}, fmt.Errorf("create audio clip failed (browser patch + Live 12.0.5+ required): %w", err)
	}
	if err := parseCreateAudioClipResponse(res, input.TrackIndex, input.ClipIndex); err != nil {
		return LoadSpliceSampleOutput{}, err
	}

	fired := false
	if input.Fire {
		if err := client.Send("/live/clip_slot/fire", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
			return LoadSpliceSampleOutput{}, fmt.Errorf("fire clip: %w", err)
		}
		fired = true
	}
	return LoadSpliceSampleOutput{
		TrackIndex:   input.TrackIndex,
		ClipIndex:    input.ClipIndex,
		AbsolutePath: abs,
		Fired:        fired,
		Note:         "Loaded from local disk into a session audio clip.",
	}, nil
}

func querySpliceSlotHasClip(client spliceLoadClient, trackIndex, clipIndex int) (bool, error) {
	res, err := client.Query("/live/clip_slot/get/has_clip", int32(trackIndex), int32(clipIndex))
	if err != nil {
		return false, err
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, err
	}
	return abletonosc.AsBool(res[2])
}

func resolveSpliceSamplePath(configured, absolutePath, relativePath string) (string, error) {
	absolutePath = strings.TrimSpace(absolutePath)
	relativePath = strings.TrimSpace(relativePath)
	if absolutePath == "" && relativePath == "" {
		return "", errors.New("absolute_path or relative_path is required")
	}
	if absolutePath != "" {
		if !filepath.IsAbs(absolutePath) {
			return "", errors.New("absolute_path must be absolute")
		}
		return filepath.Clean(absolutePath), nil
	}
	lib, err := splice.Resolve(configured)
	if err != nil {
		return "", err
	}
	if relativePath == "" || relativePath == "." {
		return "", errors.New("relative_path is empty")
	}
	abs := filepath.Clean(filepath.Join(lib.Path, filepath.FromSlash(relativePath)))
	rel, err := filepath.Rel(lib.Path, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("relative_path must stay inside the Splice library")
	}
	return abs, nil
}

func parseCreateAudioClipResponse(res []interface{}, trackIndex, clipIndex int) error {
	if len(res) == 0 {
		return errors.New("empty create_audio_clip reply")
	}
	if status, ok := res[0].(string); ok && status == "error" {
		return fmt.Errorf("create_audio_clip failed: %v", res)
	}
	if len(res) < 3 {
		return fmt.Errorf("unexpected create_audio_clip reply: %v", res)
	}
	status, err := abletonosc.AsString(res[2])
	if err != nil {
		return fmt.Errorf("unexpected create_audio_clip reply: %v", res)
	}
	switch status {
	case "created":
		return nil
	case "unsupported":
		detail := "create_audio_clip requires Ableton Live 12.0.5+"
		if len(res) > 3 {
			if msg, msgErr := abletonosc.AsString(res[3]); msgErr == nil && msg != "" {
				detail = msg
			}
		}
		return errors.New(detail)
	case "not_audio_track":
		return fmt.Errorf("track %d is not an audio track", trackIndex)
	case "invalid_track_index":
		return fmt.Errorf("invalid track_index %d", trackIndex)
	case "invalid_clip_index":
		return fmt.Errorf("invalid clip_index %d", clipIndex)
	case "error":
		detail := "create_audio_clip failed"
		if len(res) > 3 {
			if msg, msgErr := abletonosc.AsString(res[3]); msgErr == nil && msg != "" {
				detail = msg
			}
		}
		return errors.New(detail)
	default:
		return fmt.Errorf("create_audio_clip failed: status=%s reply=%v", status, res)
	}
}
