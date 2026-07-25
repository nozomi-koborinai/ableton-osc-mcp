package notation

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Parse reads notation text back into a clip. Whitespace between fields is free,
// so hand-aligned columns read the same as canonical output.
func Parse(text string) (Clip, error) {
	var c Clip
	var beatsPerBar float64
	headerSeen := false

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		// A '#' only opens a comment at the start of a line; anywhere else it is
		// part of a sharp note name.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !headerSeen {
			parsed, err := parseHeader(line)
			if err != nil {
				return Clip{}, fmt.Errorf("line %d: %w", i+1, err)
			}
			bpb, err := BeatsPerBar(parsed.SigNum, parsed.SigDen)
			if err != nil {
				return Clip{}, fmt.Errorf("line %d: %w", i+1, err)
			}
			c, beatsPerBar, headerSeen = parsed, bpb, true
			continue
		}
		note, err := parseNoteLine(line, beatsPerBar)
		if err != nil {
			return Clip{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		c.Notes = append(c.Notes, note)
	}

	if !headerSeen {
		return Clip{}, errors.New(`missing clip header; the first line must look like clip "Name" bars=4 sig=4/4`)
	}
	return c, nil
}

// parseHeader reads: clip "Name" bars=4 sig=4/4
func parseHeader(line string) (Clip, error) {
	rest, ok := strings.CutPrefix(line, "clip ")
	if !ok {
		return Clip{}, errors.New(`header must start with: clip "Name"`)
	}
	name, rest, err := cutQuoted(strings.TrimSpace(rest))
	if err != nil {
		return Clip{}, err
	}

	c := Clip{Name: name, Bars: -1, SigNum: -1, SigDen: -1}
	for field := range strings.FieldsSeq(rest) {
		switch {
		case strings.HasPrefix(field, "bars="):
			bars, err := strconv.Atoi(strings.TrimPrefix(field, "bars="))
			if err != nil || bars < 0 {
				return Clip{}, fmt.Errorf("bars must be a whole number, got %q", field)
			}
			c.Bars = bars
		case strings.HasPrefix(field, "sig="):
			num, den, ok := strings.Cut(strings.TrimPrefix(field, "sig="), "/")
			if !ok {
				return Clip{}, fmt.Errorf("sig must look like 4/4, got %q", field)
			}
			n, errNum := strconv.Atoi(num)
			d, errDen := strconv.Atoi(den)
			if errNum != nil || errDen != nil || n <= 0 || d <= 0 {
				return Clip{}, fmt.Errorf("sig must look like 4/4, got %q", field)
			}
			c.SigNum, c.SigDen = n, d
		default:
			return Clip{}, fmt.Errorf("unknown header field %q", field)
		}
	}
	if c.Bars < 0 {
		return Clip{}, errors.New("header is missing bars=")
	}
	if c.SigNum < 0 {
		return Clip{}, errors.New("header is missing sig=")
	}
	return c, nil
}

// cutQuoted takes a Go-quoted string off the front and returns it unquoted along
// with whatever follows.
func cutQuoted(s string) (value string, rest string, err error) {
	if s == "" || s[0] != '"' {
		return "", "", errors.New("clip name must be in double quotes")
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			v, err := strconv.Unquote(s[:i+1])
			if err != nil {
				return "", "", fmt.Errorf("bad clip name: %w", err)
			}
			return v, s[i+1:], nil
		}
	}
	return "", "", errors.New("clip name is missing its closing quote")
}

// parseNoteLine reads: <position> <pitch> <length> v<velocity> [-]
func parseNoteLine(line string, beatsPerBar float64) (Note, error) {
	fields := strings.Fields(line)
	if len(fields) < 4 || len(fields) > 5 {
		return Note{}, fmt.Errorf("expected '<position> <pitch> <length> v<velocity> [-]', got %d fields", len(fields))
	}
	start, err := ParsePosition(fields[0], beatsPerBar)
	if err != nil {
		return Note{}, err
	}
	pitch, err := ParsePitch(fields[1])
	if err != nil {
		return Note{}, err
	}
	duration, err := ParseDuration(fields[2])
	if err != nil {
		return Note{}, err
	}
	velocityText, ok := strings.CutPrefix(fields[3], "v")
	if !ok {
		return Note{}, fmt.Errorf("velocity must look like v100, got %q", fields[3])
	}
	velocity, err := strconv.Atoi(velocityText)
	if err != nil || velocity < 1 || velocity > 127 {
		return Note{}, fmt.Errorf("velocity must be a whole number from 1 to 127, got %q", fields[3])
	}
	mute := false
	if len(fields) == 5 {
		if fields[4] != "-" {
			return Note{}, fmt.Errorf("the only trailing mark is '-' for a muted note, got %q", fields[4])
		}
		mute = true
	}
	return Note{Pitch: pitch, StartTime: start, Duration: duration, Velocity: velocity, Mute: mute}, nil
}
