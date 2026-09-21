package audioanalyze

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestWriteWAVReadsBackWhatWasWritten(t *testing.T) {
	t.Parallel()

	left := resampleTestTone(440, 0.5, 44100, 0.1)
	right := resampleTestTone(660, 0.25, 44100, 0.1)
	for _, tc := range []struct {
		bits      int
		tolerance float64
	}{{24, 1.0 / (1 << 23)}, {16, 1.0 / (1 << 15)}} {
		var buf bytes.Buffer
		if err := writeWAV(&buf, [][]float64{left, right}, 44100, tc.bits, nil); err != nil {
			t.Fatalf("%d bit: writeWAV() error = %v", tc.bits, err)
		}
		raw := buf.Bytes()
		if got := binary.LittleEndian.Uint32(raw[4:8]); int(got) != len(raw)-8 {
			t.Errorf("%d bit: RIFF size = %d, want %d", tc.bits, got, len(raw)-8)
		}
		if got := binary.LittleEndian.Uint16(raw[34:36]); int(got) != tc.bits {
			t.Errorf("bits per sample in the header = %d, want %d", got, tc.bits)
		}
		audio, format, err := loadAudio(bytes.NewReader(raw))
		if err != nil || format != "wav" || audio.sampleRate != 44100 || audio.channels != 2 || len(audio.left) != len(left) {
			t.Fatalf("%d bit: read back %d frames at %d Hz, %d ch, %q, %v", tc.bits, len(audio.left), audio.sampleRate, audio.channels, format, err)
		}
		for i := range left {
			if math.Abs(audio.left[i]-left[i]) > tc.tolerance || math.Abs(audio.right[i]-right[i]) > tc.tolerance {
				t.Fatalf("%d bit: frame %d reads %v / %v, wrote %v / %v", tc.bits, i, audio.left[i], audio.right[i], left[i], right[i])
			}
		}
	}
}

func TestWriteWAVClampsInsteadOfWrapping(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := writeWAV(&buf, [][]float64{{1.5, -1.5, 1.0, -1.0}}, 48000, 24, nil); err != nil {
		t.Fatal(err)
	}
	audio, _, err := loadAudio(bytes.NewReader(buf.Bytes()))
	if err != nil || audio.channels != 1 {
		t.Fatalf("read back: %v (%d ch)", err, audio.channels)
	}
	// An integer overflow would turn +1.5 into a large negative value.
	if audio.left[0] < 0.999 || audio.left[1] > -0.999 || audio.left[2] < 0.999 || audio.left[3] > -0.999 {
		t.Errorf("read back %v, want full scale with the sign kept", audio.left)
	}
	// Three bytes per frame, four frames of mono: the data chunk is padded to an even length.
	if len(buf.Bytes())%2 != 0 {
		t.Errorf("file length %d is odd", len(buf.Bytes()))
	}
}

func TestWriteWAVDithersSixteenBit(t *testing.T) {
	t.Parallel()

	// A level a quarter of a step above zero vanishes when it is simply rounded.
	// With dither it survives as the mean of the noise.
	quiet := make([]float64, 48000)
	for i := range quiet {
		quiet[i] = 0.25 / (1 << 15)
	}
	state := uint64(1)
	uniform := func() float64 { // a small deterministic generator, [0, 1)
		state = state*6364136223846793005 + 1442695040888963407
		return float64(state>>11) / (1 << 53)
	}
	dither := func() float64 { return uniform() - uniform() } // triangular, +-1 step
	var buf bytes.Buffer
	if err := writeWAV(&buf, [][]float64{quiet}, 48000, 16, dither); err != nil {
		t.Fatal(err)
	}
	audio, _, err := loadAudio(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	mean := 0.0
	for _, v := range audio.left {
		mean += v
	}
	mean = mean / float64(len(audio.left)) * (1 << 15)
	if math.Abs(mean-0.25) > 0.03 {
		t.Errorf("mean of the dithered signal = %.3f steps, want 0.25", mean)
	}
}

func TestWriteWAVRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	for name, write := range map[string]func() error{
		"no channels":     func() error { return writeWAV(&buf, nil, 44100, 24, nil) },
		"unequal lengths": func() error { return writeWAV(&buf, [][]float64{{0, 0}, {0}}, 44100, 24, nil) },
		"8 bit":           func() error { return writeWAV(&buf, [][]float64{{0}}, 44100, 8, nil) },
		"no rate":         func() error { return writeWAV(&buf, [][]float64{{0}}, 0, 24, nil) },
	} {
		if err := write(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
