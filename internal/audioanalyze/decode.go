package audioanalyze

import (
	"bytes"
	"fmt"
	"io"
)

// loadAudio sniffs the container from its magic bytes and decodes it. The
// returned format is "wav" or "aiff". File extensions are not trusted.
func loadAudio(r io.Reader) (wavAudio, string, error) {
	var head [12]byte
	n, err := io.ReadFull(r, head[:])
	if err != nil {
		return wavAudio{}, "", fmt.Errorf("read audio header: %w", err)
	}
	full := io.MultiReader(bytes.NewReader(head[:n]), r)
	switch {
	case string(head[0:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		audio, err := loadWAV(full)
		return audio, "wav", err
	case string(head[0:4]) == "FORM" && (string(head[8:12]) == "AIFF" || string(head[8:12]) == "AIFC"):
		audio, err := loadAIFF(full)
		return audio, "aiff", err
	default:
		return wavAudio{}, "", fmt.Errorf("%w: not a WAV or AIFF file", ErrUnsupportedFormat)
	}
}
