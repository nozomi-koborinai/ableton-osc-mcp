package audioanalyze

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// writeWAV writes PCM audio as a RIFF/WAVE file, 16 or 24 bit. Values beyond
// full scale are clamped, never wrapped. dither, when given, returns the noise
// to add before rounding, in steps of the target word length (triangular,
// +-1 step, for a 16-bit file; nil for 24 bit, whose steps are far below
// anything a converter resolves).
func writeWAV(w io.Writer, channels [][]float64, sampleRate, bitDepth int, dither func() float64) error {
	if len(channels) == 0 || len(channels) > 2 {
		return fmt.Errorf("a WAV delivery has 1 or 2 channels, got %d", len(channels))
	}
	if bitDepth != 16 && bitDepth != 24 {
		return fmt.Errorf("bit depth must be 16 or 24, got %d", bitDepth)
	}
	if sampleRate <= 0 {
		return errors.New("sample rate must be positive")
	}
	frames := len(channels[0])
	for _, ch := range channels {
		if len(ch) != frames {
			return errors.New("channels differ in length")
		}
	}

	width := bitDepth / 8
	blockAlign := width * len(channels)
	dataBytes := frames * blockAlign
	padding := dataBytes % 2 // chunks are word aligned
	if uint64(36+dataBytes+padding) > math.MaxUint32 {
		return errors.New("audio too long for a WAV file")
	}

	out := bufio.NewWriter(w)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataBytes+padding))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:24], uint16(len(channels)))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*blockAlign))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitDepth))
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataBytes))
	if _, err := out.Write(header); err != nil {
		return err
	}

	scale := float64(int64(1) << (bitDepth - 1))
	sample := make([]byte, 3)
	for i := 0; i < frames; i++ {
		for _, ch := range channels {
			v := ch[i] * scale
			if dither != nil {
				v += dither()
			}
			q := int32(math.Max(-scale, math.Min(scale-1, math.Round(v))))
			sample[0], sample[1], sample[2] = byte(q), byte(q>>8), byte(q>>16)
			if _, err := out.Write(sample[:width]); err != nil {
				return err
			}
		}
	}
	if padding == 1 {
		if err := out.WriteByte(0); err != nil {
			return err
		}
	}
	return out.Flush()
}
