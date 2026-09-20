package audioanalyze

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// ErrUnsupportedFormat marks audio this package cannot decode: an unknown
// container, or a compressed AIFF-C payload.
var ErrUnsupportedFormat = errors.New("unsupported audio format")

// loadAIFF decodes AIFF and AIFF-C audio. Live on macOS records AIFF (24-bit
// big-endian) by default, so anything bounced inside Live arrives like this.
func loadAIFF(f io.Reader) (wavAudio, error) {
	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return wavAudio{}, fmt.Errorf("read aiff header: %w", err)
	}
	form := string(header[8:12])
	if string(header[0:4]) != "FORM" || (form != "AIFF" && form != "AIFC") {
		return wavAudio{}, errors.New("not a FORM/AIFF file")
	}

	var (
		channels    int
		bits        int
		sampleRate  float64
		compression = "NONE"
		data        []byte
	)
	for {
		var chunkHeader [8]byte
		if _, err := io.ReadFull(f, chunkHeader[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return wavAudio{}, err
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.BigEndian.Uint32(chunkHeader[4:8])

		if chunkID == "SSND" {
			// Trust the declared size so chunks after the sound data (markers,
			// ID3) are not decoded as audio; a zero size means "to the end".
			var src io.Reader = f
			if chunkSize > 0 {
				src = io.LimitReader(f, int64(chunkSize))
			}
			payload, err := io.ReadAll(src)
			if err != nil {
				return wavAudio{}, fmt.Errorf("read SSND chunk: %w", err)
			}
			if len(payload) < 8 {
				return wavAudio{}, errors.New("invalid SSND chunk")
			}
			offset := int(binary.BigEndian.Uint32(payload[0:4]))
			if 8+offset > len(payload) {
				return wavAudio{}, errors.New("invalid SSND offset")
			}
			data = payload[8+offset:]
		} else {
			if chunkSize > maxChunkBytes {
				return wavAudio{}, fmt.Errorf("%s chunk too large (%d bytes)", chunkID, chunkSize)
			}
			payload := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, payload); err != nil {
				return wavAudio{}, fmt.Errorf("read %s chunk: %w", chunkID, err)
			}
			if chunkID == "COMM" {
				if len(payload) < 18 {
					return wavAudio{}, errors.New("invalid COMM chunk")
				}
				channels = int(binary.BigEndian.Uint16(payload[0:2]))
				bits = int(binary.BigEndian.Uint16(payload[6:8]))
				var ext [10]byte
				copy(ext[:], payload[8:18])
				sampleRate = extendedToFloat64(ext)
				if form == "AIFC" && len(payload) >= 22 {
					compression = string(payload[18:22])
				}
			}
		}
		// Chunks are word-aligned.
		if chunkSize%2 == 1 {
			var pad [1]byte
			_, _ = f.Read(pad[:])
		}
	}
	if len(data) == 0 || channels == 0 || sampleRate <= 0 {
		return wavAudio{}, errors.New("aiff missing COMM/SSND")
	}

	bytesPerSample, decode, err := aiffSampleDecoder(bits, compression)
	if err != nil {
		return wavAudio{}, err
	}
	chans, err := deinterleave(data, channels, bytesPerSample, decode)
	if err != nil {
		return wavAudio{}, err
	}

	audio := wavAudio{
		mono:       downmix(chans),
		left:       chans[0],
		sampleRate: int(math.Round(sampleRate)),
		channels:   channels,
	}
	if len(chans) >= 2 {
		audio.right = chans[1]
	} else {
		audio.right = chans[0]
	}
	return audio, nil
}

// aiffSampleDecoder returns the byte width and a decode function for one AIFF
// sample. Plain AIFF is big-endian PCM; AIFF-C adds "sowt" (little-endian PCM)
// and "fl32" (big-endian float).
func aiffSampleDecoder(bits int, compression string) (int, func([]byte) float64, error) {
	signExtend24 := func(v int32) float64 {
		if v&0x800000 != 0 {
			v |= ^0xFFFFFF
		}
		return float64(v) / 8388608.0
	}
	switch compression {
	case "NONE", "twos":
		switch bits {
		case 8:
			return 1, func(b []byte) float64 { return float64(int8(b[0])) / 128.0 }, nil
		case 16:
			return 2, func(b []byte) float64 { return float64(int16(binary.BigEndian.Uint16(b[0:2]))) / 32768.0 }, nil
		case 24:
			return 3, func(b []byte) float64 {
				return signExtend24(int32(b[0])<<16 | int32(b[1])<<8 | int32(b[2]))
			}, nil
		case 32:
			return 4, func(b []byte) float64 { return float64(int32(binary.BigEndian.Uint32(b[0:4]))) / 2147483648.0 }, nil
		}
	case "sowt":
		switch bits {
		case 16:
			return 2, func(b []byte) float64 { return float64(int16(binary.LittleEndian.Uint16(b[0:2]))) / 32768.0 }, nil
		case 24:
			return 3, func(b []byte) float64 {
				return signExtend24(int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16)
			}, nil
		case 32:
			return 4, func(b []byte) float64 { return float64(int32(binary.LittleEndian.Uint32(b[0:4]))) / 2147483648.0 }, nil
		}
	case "fl32", "FL32":
		if bits == 32 {
			return 4, func(b []byte) float64 { return float64(math.Float32frombits(binary.BigEndian.Uint32(b[0:4]))) }, nil
		}
	}
	return 0, nil, fmt.Errorf("%w: aiff compression=%q bits=%d (use WAV or uncompressed AIFF)", ErrUnsupportedFormat, compression, bits)
}

// extendedToFloat64 converts the 80-bit IEEE 754 extended float AIFF uses for
// the sample rate.
func extendedToFloat64(b [10]byte) float64 {
	sign := 1.0
	if b[0]&0x80 != 0 {
		sign = -1
	}
	exp := int(b[0]&0x7f)<<8 | int(b[1])
	var mant uint64
	for i := 2; i < 10; i++ {
		mant = mant<<8 | uint64(b[i])
	}
	if exp == 0 && mant == 0 {
		return 0
	}
	return sign * math.Ldexp(float64(mant), exp-16383-63)
}
