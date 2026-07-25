// Command measure_roundtrip writes a fixed set of notes into an empty Live clip,
// reads them back, and reports the largest difference seen in start time and
// duration. The number it prints is what the notation round-trip check may treat
// as a match; without it, "the round trip closes" is an unbacked claim.
//
// It also prints the song signature so we can confirm both the numerator and the
// denominator are reachable over OSC.
//
// Usage: go run ./cmd/measure_roundtrip -track 0 -slot 0
// The target slot must be empty and on a MIDI track.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// printSessionGrid reports which clip slots already hold something, so the
// destructive run can be aimed at an empty one instead of guessing.
func printSessionGrid(client *abletonosc.Client) error {
	names, err := client.Query("/live/song/get/track_names")
	if err != nil {
		return fmt.Errorf("track names: %w", err)
	}
	scenesRes, err := client.Query("/live/song/get/num_scenes")
	if err != nil {
		return fmt.Errorf("num scenes: %w", err)
	}
	numScenes, err := abletonosc.AsInt(scenesRes[0])
	if err != nil {
		return fmt.Errorf("num scenes: %w", err)
	}

	fmt.Printf("%d tracks, %d scenes. '.' is an empty slot, 'X' holds a clip.\n\n", len(names), numScenes)
	for t, name := range names {
		row := make([]byte, 0, numScenes)
		for s := 0; s < numScenes; s++ {
			res, err := client.Query("/live/clip_slot/get/has_clip", int32(t), int32(s))
			if err != nil {
				row = append(row, '?')
				continue
			}
			has, err := abletonosc.AsBool(res[2])
			if err != nil || !has {
				row = append(row, '.')
				continue
			}
			row = append(row, 'X')
		}
		fmt.Printf("track %2d  %-24v %s\n", t, name, row)
	}
	return nil
}

// probe notes mix positions that sit on the grid with positions that do not,
// because rounding is most likely to show up off the grid. They also reach far
// into a long clip: OSC carries these values as float32, whose absolute error
// grows with magnitude, so a tolerance measured only near the clip start would
// be too tight for a note at bar 64.
var probeStarts = []float64{0, 0.5, 1.0 / 3.0, 1.25, 2.0 / 3.0, 3.7519, 127.3333, 255.0625, 511.7519}
var probeDurations = []float64{0.5, 0.25, 1.0 / 3.0, 0.75, 0.1875, 1.9377, 2.3333, 0.5, 1.9377}

// probeClipBeats has to be long enough to hold the farthest probe note.
const probeClipBeats = 520

func main() {
	host := flag.String("host", "127.0.0.1", "AbletonOSC host")
	port := flag.Int("port", 11000, "AbletonOSC port")
	localPort := flag.Int("local-port", 11001, "local reply port")
	track := flag.Int("track", 0, "MIDI track index")
	slot := flag.Int("slot", 0, "clip slot index; must be empty")
	inspect := flag.Bool("inspect", false, "only print the session grid and exit; writes nothing")
	flag.Parse()

	client, err := abletonosc.NewClient(*host, *port, *localPort, 2*time.Second)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer func() { _ = client.Close() }()

	if *inspect {
		if err := printSessionGrid(client); err != nil {
			log.Fatalf("inspect: %v", err)
		}
		return
	}

	for _, addr := range []string{"/live/song/get/signature_numerator", "/live/song/get/signature_denominator"} {
		res, err := client.Query(addr)
		if err != nil {
			log.Printf("WARN %s is not reachable: %v", addr, err)
			continue
		}
		fmt.Printf("%s = %v\n", addr, res)
	}

	res, err := client.Query("/live/clip_slot/get/has_clip", int32(*track), int32(*slot))
	if err != nil {
		log.Fatalf("check slot: %v", err)
	}
	if has, _ := abletonosc.AsBool(res[2]); has {
		log.Fatalf("slot [%d,%d] already holds a clip; pick an empty one (run with -inspect to see the grid)", *track, *slot)
	}

	if err := client.Send("/live/clip_slot/create_clip", int32(*track), int32(*slot), float32(probeClipBeats)); err != nil {
		log.Fatalf("create clip: %v", err)
	}
	// The probe clip is scratch, so remove it on the way out and leave the set as
	// it was found.
	defer func() {
		if err := client.Send("/live/clip_slot/delete_clip", int32(*track), int32(*slot)); err != nil {
			log.Printf("WARN could not remove the probe clip at [%d,%d]: %v", *track, *slot, err)
		}
	}()
	time.Sleep(200 * time.Millisecond)

	args := []interface{}{int32(*track), int32(*slot)}
	for i, start := range probeStarts {
		args = append(args, int32(60+i), float32(start), float32(probeDurations[i]), int32(100), false)
	}
	if err := client.Send("/live/clip/add/notes", args...); err != nil {
		log.Fatalf("add notes: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	res, err = client.Query("/live/clip/get/notes", int32(*track), int32(*slot))
	if err != nil {
		log.Fatalf("get notes: %v", err)
	}
	if len(res) < 2 {
		log.Fatalf("unexpected reply: %v", res)
	}
	payload := res[2:]
	if len(payload) != len(probeStarts)*5 {
		log.Fatalf("expected %d notes, got %d values back", len(probeStarts), len(payload))
	}

	var worst float64
	for i := 0; i < len(payload); i += 5 {
		gotStart, err := abletonosc.AsFloat64(payload[i+1])
		if err != nil {
			log.Fatalf("start time: %v", err)
		}
		gotDur, err := abletonosc.AsFloat64(payload[i+2])
		if err != nil {
			log.Fatalf("duration: %v", err)
		}
		n := i / 5
		ds := math.Abs(gotStart - probeStarts[n])
		dd := math.Abs(gotDur - probeDurations[n])
		fmt.Printf("note %d: start %.9f -> %.9f (d=%.9f)  dur %.9f -> %.9f (d=%.9f)\n",
			n, probeStarts[n], gotStart, ds, probeDurations[n], gotDur, dd)
		worst = math.Max(worst, math.Max(ds, dd))
	}

	fmt.Printf("\nlargest difference: %.9f beats\n", worst)
	if worst > 0.001 {
		fmt.Fprintln(os.Stderr, "difference is larger than 0.001 beats — stop and reconsider before baking this in")
	}
}
