package tools

import (
	"errors"
	"fmt"
)

func validateTrackClipIndices(trackIndex int, clipIndex int) error {
	if trackIndex < 0 || clipIndex < 0 {
		return errors.New("track_index and clip_index must be >= 0")
	}
	return nil
}

func ensureResponseLen(res []interface{}, min int) error {
	if len(res) < min {
		return fmt.Errorf("unexpected response: %v", res)
	}
	return nil
}

func toStringSlice(values []interface{}) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, fmt.Sprint(v))
	}
	return out
}
