package audioanalyze

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

// Sample rates as 80-bit IEEE 754 extended floats, exactly as AIFF stores them.
var (
	ext44100 = [10]byte{0x40, 0x0e, 0xac, 0x44, 0, 0, 0, 0, 0, 0}
	ext48000 = [10]byte{0x40, 0x0e, 0xbb, 0x80, 0, 0, 0, 0, 0, 0}
)

// buildAIFF assembles a FORM/AIFF or FORM/AIFC file. compression is ignored
// for plain AIFF.
func buildAIFF(form, compression string, channels, bits int, rate [10]byte, pcm []byte) []byte {
	comm := new(bytes.Buffer)
	_ = binary.Write(comm, binary.BigEndian, uint16(channels))
	frames := 0
	if channels > 0 && bits > 0 {
		frames = len(pcm) / (channels * bits / 8)
	}
	_ = binary.Write(comm, binary.BigEndian, uint32(frames))
	_ = binary.Write(comm, binary.BigEndian, uint16(bits))
	comm.Write(rate[:])
	if form == "AIFC" {
		comm.WriteString(compression)
		comm.Write([]byte{0, 0}) // empty pascal string, padded to even length
	}

	ssnd := new(bytes.Buffer)
	_ = binary.Write(ssnd, binary.BigEndian, uint32(0)) // offset
	_ = binary.Write(ssnd, binary.BigEndian, uint32(0)) // block size
	ssnd.Write(pcm)

	body := new(bytes.Buffer)
	body.WriteString(form)
	for _, chunk := range []struct {
		id   string
		data []byte
	}{{"COMM", comm.Bytes()}, {"SSND", ssnd.Bytes()}} {
		body.WriteString(chunk.id)
		_ = binary.Write(body, binary.BigEndian, uint32(len(chunk.data)))
		body.Write(chunk.data)
		if len(chunk.data)%2 == 1 {
			body.WriteByte(0)
		}
	}

	out := new(bytes.Buffer)
	out.WriteString("FORM")
	_ = binary.Write(out, binary.BigEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestLoadAIFFDecodesSupportedEncodings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		form        string
		compression string
		channels    int
		bits        int
		rate        [10]byte
		pcm         []byte
		wantRate    int
		wantLeft    []float64
		wantRight   []float64
	}{
		{
			name: "16-bit big-endian stereo", form: "AIFF", channels: 2, bits: 16, rate: ext44100,
			// L=+0.5, R=+0.25, then L=-0.5, R=-0.25
			pcm:      []byte{0x40, 0x00, 0x20, 0x00, 0xc0, 0x00, 0xe0, 0x00},
			wantRate: 44100, wantLeft: []float64{0.5, -0.5}, wantRight: []float64{0.25, -0.25},
		},
		{
			name: "24-bit big-endian stereo as Live records it", form: "AIFF", channels: 2, bits: 24, rate: ext48000,
			pcm:      []byte{0x40, 0x00, 0x00, 0xc0, 0x00, 0x00, 0x20, 0x00, 0x00, 0xe0, 0x00, 0x00},
			wantRate: 48000, wantLeft: []float64{0.5, 0.25}, wantRight: []float64{-0.5, -0.25},
		},
		{
			name: "AIFF-C sowt is little-endian", form: "AIFC", compression: "sowt", channels: 1, bits: 16, rate: ext44100,
			pcm:      []byte{0x00, 0x40, 0x00, 0xc0},
			wantRate: 44100, wantLeft: []float64{0.5, -0.5}, wantRight: []float64{0.5, -0.5},
		},
		{
			name: "AIFF-C fl32 is big-endian float", form: "AIFC", compression: "fl32", channels: 1, bits: 32, rate: ext48000,
			// 0.5 = 0x3f000000, -0.25 = 0xbe800000
			pcm:      []byte{0x3f, 0x00, 0x00, 0x00, 0xbe, 0x80, 0x00, 0x00},
			wantRate: 48000, wantLeft: []float64{0.5, -0.25}, wantRight: []float64{0.5, -0.25},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loadAIFF(bytes.NewReader(buildAIFF(tc.form, tc.compression, tc.channels, tc.bits, tc.rate, tc.pcm)))
			if err != nil {
				t.Fatalf("loadAIFF() error = %v", err)
			}
			if got.sampleRate != tc.wantRate || got.channels != tc.channels {
				t.Fatalf("rate/channels = %d/%d, want %d/%d", got.sampleRate, got.channels, tc.wantRate, tc.channels)
			}
			for i := range tc.wantLeft {
				if math.Abs(got.left[i]-tc.wantLeft[i]) > 1e-6 || math.Abs(got.right[i]-tc.wantRight[i]) > 1e-6 {
					t.Errorf("frame %d = L %v R %v, want L %v R %v", i, got.left[i], got.right[i], tc.wantLeft[i], tc.wantRight[i])
				}
			}
		})
	}
}

func TestLoadAIFFRejectsCompressedAudio(t *testing.T) {
	t.Parallel()

	_, err := loadAIFF(bytes.NewReader(buildAIFF("AIFC", "ima4", 1, 16, ext44100, []byte{0, 0, 0, 0})))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("error = %v, want ErrUnsupportedFormat", err)
	}
	if !strings.Contains(err.Error(), "ima4") {
		t.Errorf("error should name the compression type: %v", err)
	}
}

func TestLoadAudioSniffsContainerFromMagic(t *testing.T) {
	t.Parallel()

	aiff := buildAIFF("AIFF", "", 1, 16, ext44100, []byte{0x40, 0x00, 0xc0, 0x00})
	if _, format, err := loadAudio(bytes.NewReader(aiff)); err != nil || format != "aiff" {
		t.Errorf("AIFF: format = %q, err = %v, want aiff", format, err)
	}
	wav := clickWAVBytes(t, 44100, 120, 1)
	if _, format, err := loadAudio(bytes.NewReader(wav)); err != nil || format != "wav" {
		t.Errorf("WAV: format = %q, err = %v, want wav", format, err)
	}
	if _, _, err := loadAudio(bytes.NewReader([]byte("ID3\x04not audio at all"))); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("garbage: error = %v, want ErrUnsupportedFormat", err)
	}
}

func TestValidateLocalAudioPathAcceptsAIFF(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"/tmp/a.wav", "/tmp/a.aif", "/tmp/a.AIFF"} {
		if _, err := validateLocalAudioPath(p); err != nil {
			t.Errorf("validateLocalAudioPath(%q) error = %v", p, err)
		}
	}
	if _, err := validateLocalAudioPath("/tmp/a.mp3"); err == nil {
		t.Error("validateLocalAudioPath(.mp3) should fail")
	}
}
