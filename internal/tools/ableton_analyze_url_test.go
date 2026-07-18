package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

func TestAnalyzeAudioURLRejectsNonURL(t *testing.T) {
	t.Parallel()

	_, err := audioanalyze.AnalyzeURL(context.Background(), "/local/file.wav", 0)
	if err == nil {
		t.Fatal("expected rejection for non-http input")
	}
	if !strings.Contains(err.Error(), "http") {
		t.Errorf("error should mention http scheme requirement: %v", err)
	}
}
