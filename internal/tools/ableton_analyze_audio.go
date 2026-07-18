package tools

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

type AnalyzeLocalAudioInput struct {
	Path         string   `json:"path" jsonschema:"description=Absolute local path to a .wav file you already have (no URLs)"`
	ProjectTempo *float64 `json:"project_tempo,omitempty" jsonschema:"description=Optional project BPM to estimate length in bars,minimum=20,maximum=400"`
}

type AnalyzeLocalAudioOutput struct {
	Path              string  `json:"path"`
	Format            string  `json:"format"`
	DurationSec       float64 `json:"duration_sec"`
	SampleRate        int     `json:"sample_rate"`
	Channels          int     `json:"channels"`
	PeakLevel         float64 `json:"peak_level"`
	RMSLevel          float64 `json:"rms_level"`
	EstimatedBPM      float64 `json:"estimated_bpm"`
	BPMConfidence     float64 `json:"bpm_confidence"`
	OnsetCount        int     `json:"onset_count"`
	SuggestedWarpMode string  `json:"suggested_warp_mode"`
	LengthBarsAtBPM   float64 `json:"length_bars_at_project_tempo,omitempty"`
	Note              string  `json:"note"`
	NextStep          string  `json:"next_step"`
}

func NewAbletonAnalyzeLocalAudio(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_analyze_local_audio",
		"Analyze a local .wav file for sampling placement (duration, levels, estimated BPM). Does not download URLs or extract melodies/notes.",
		func(_ *ai.ToolContext, input AnalyzeLocalAudioInput) (AnalyzeLocalAudioOutput, error) {
			return analyzeLocalAudio(input)
		},
	)
}

func analyzeLocalAudio(input AnalyzeLocalAudioInput) (AnalyzeLocalAudioOutput, error) {
	projectTempo := 0.0
	if input.ProjectTempo != nil {
		projectTempo = *input.ProjectTempo
	}
	got, err := audioanalyze.AnalyzeFile(input.Path, projectTempo)
	if err != nil {
		return AnalyzeLocalAudioOutput{}, err
	}
	return AnalyzeLocalAudioOutput{
		Path:              got.Path,
		Format:            got.Format,
		DurationSec:       got.DurationSec,
		SampleRate:        got.SampleRate,
		Channels:          got.Channels,
		PeakLevel:         got.PeakLevel,
		RMSLevel:          got.RMSLevel,
		EstimatedBPM:      got.EstimatedBPM,
		BPMConfidence:     got.BPMConfidence,
		OnsetCount:        got.OnsetCount,
		SuggestedWarpMode: got.SuggestedWarpMode,
		LengthBarsAtBPM:   got.LengthBarsAtBPM,
		Note:              got.Note,
		NextStep:          "If you load this sample into Live, use ableton_match_clip_tempo with the suggested_warp_mode so it follows the project tempo.",
	}, nil
}
