// Command genfixture writes a synthetic ghostReplays tree so the server and UI
// can be exercised without a BeamNG install.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

func main() {
	out := flag.String("out", "testdata/ghostReplays", "directory to write the fixture into")
	size := flag.Float64("size", 1, "circuit size multiplier; raises the sample count per lap")
	flag.Parse()

	level := "east_coast_usa"
	starts := []struct {
		id       string
		name     string
		startKey string
		laps     int
		seed     int64
		p2p      bool
	}{
		{"s001", "Coast loop", "s001", 5, 1, false},
		{"s002", "Coast loop – short cut", "s001", 3, 7, true},
		{"s003", "Hill climb", "s003", 2, 11, false},
	}

	for _, start := range starts {
		base := filepath.Join(*out, "freeRoam", level, "starts", start.id)
		must(os.MkdirAll(filepath.Join(base, "ghostracer.save.ghosts"), 0o755))

		manifest := map[string]any{
			"formatVersion": 1,
			"nextId":        start.laps + 1,
			"displayMode":   "best",
			"startLine": map[string]any{
				"level":     level,
				"position":  []float64{0, 0, 12},
				"normal":    []float64{0, 1, 0},
				"halfWidth": 8,
			},
			"ghosts": []any{},
		}
		descriptors := []any{}
		random := rand.New(rand.NewSource(start.seed))

		for lap := 1; lap <= start.laps; lap++ {
			complete := lap != start.laps || start.laps < 3
			manual := lap == 2 && start.laps > 3
			pace := 1.0 + 0.04*float64(lap-1) + 0.02*random.Float64()
			samples, lapTime := generateLap(random, pace, complete, start.seed, *size)

			envelope := map[string]any{
				"formatVersion":  2,
				"sampleInterval": 0.02,
				"duration":       lapTime,
				"lapTime":        lapTime,
				"vehicle":        vehicleFor(lap),
				"groundOffset":   0.35,
				"complete":       complete,
				"samples":        samples,
			}
			if !complete {
				envelope["incompleteReason"] = "reset"
				envelope["lapTime"] = nil
			}
			id := fmt.Sprintf("g%06d", lap)
			file := filepath.Join(base, "ghostracer.save.ghosts", id+".json")
			writeJSON(file, envelope)

			descriptor := map[string]any{
				"id":             id,
				"label":          fmt.Sprintf("Lap %d", lap),
				"duration":       lapTime,
				"sampleInterval": 0.02,
				"groundOffset":   0.35,
				"hasSpeed":       true,
				"source":         sourceFor(manual, complete),
				"complete":       complete,
				"vehicle":        vehicleFor(lap),
				"manual":         manual,
				"color":          []string{"orange", "cyan", "green", "magenta", "white"}[(lap-1)%5],
				"pinned":         lap == 1,
				"hasInputs":      true,
				"file": fmt.Sprintf(
					"ghostReplays/freeRoam/%s/starts/%s/ghostracer.save.ghosts/%s.json",
					level, start.id, id),
			}
			if complete {
				descriptor["lapTime"] = lapTime
			} else {
				descriptor["incompleteReason"] = "reset"
			}
			descriptors = append(descriptors, descriptor)
		}
		manifest["ghosts"] = descriptors
		writeJSON(filepath.Join(base, "ghostracer.save.library.json"), manifest)
	}

	registry := map[string]any{
		"formatVersion": 3,
		"revision":      12,
		"level":         level,
		"nextId":        4,
		"activeId":      "s001",
		"lines":         []any{},
	}
	lines := []any{}
	for _, start := range starts {
		line := map[string]any{
			"id":        start.id,
			"name":      start.name,
			"userNamed": true,
			"startKey":  start.startKey,
			"position":  []float64{0, 0, 12},
			"normal":    []float64{0, 1, 0},
		}
		if start.p2p {
			line["finishPosition"] = []float64{180, 40, 18}
			line["finishNormal"] = []float64{1, 0, 0}
		}
		lines = append(lines, line)
	}
	registry["lines"] = lines
	writeJSON(filepath.Join(*out, "freeRoam", level, "startLines.json"), registry)

	fmt.Printf("fixture written to %s\n", *out)
}

func vehicleFor(lap int) string {
	if lap%3 == 0 {
		return "etk800"
	}
	return "sunburst"
}

func sourceFor(manual, complete bool) string {
	switch {
	case manual:
		return "manual"
	case !complete:
		return "incomplete"
	default:
		return "lap"
	}
}

// generateLap drives a closed circuit made of straights and corners, producing
// plausible speed, throttle and brake traces at 50 Hz.
func generateLap(random *rand.Rand, pace float64, complete bool, seed int64, size float64) ([][]float64, float64) {
	const interval = 0.02
	radiusX, radiusY := 220.0*size, 130.0*size
	samples := [][]float64{}

	angle := 0.0
	time := 0.0
	gear := 3.0
	for angle < 2*math.Pi {
		// Corner radius drives the speed target, as it would on track.
		curvature := math.Abs(math.Sin(angle*2)) * 0.9
		targetSpeed := (52 - 26*curvature) / pace
		wobble := 1 + 0.02*math.Sin(angle*7+float64(seed))
		speed := targetSpeed * wobble

		x := radiusX * math.Cos(angle)
		y := radiusY*math.Sin(angle) + 30*math.Sin(angle*3)
		z := 12 + 6*math.Sin(angle*2)

		next := angle + 0.004
		dx := -radiusX * math.Sin(next)
		dy := radiusY*math.Cos(next) + 90*math.Cos(next*3)
		length := math.Hypot(dx, dy)
		if length == 0 {
			length = 1
		}

		throttle := math.Max(0, math.Min(1, 1.1-curvature*1.6))
		brake := math.Max(0, math.Min(1, (curvature-0.55)*2.2))
		if brake > 0 {
			throttle = 0
		}
		gear = math.Max(1, math.Min(6, math.Round(speed/11)))

		samples = append(samples, []float64{
			round(time, 3),
			round(x, 3), round(y, 3), round(z, 3),
			round(dx/length, 4), round(dy/length, 4), 0,
			0, 0, 1,
			round(speed, 3),
			round(throttle, 2), round(brake, 2), gear, 0, 0,
		})

		// Advance by the distance covered at the current speed.
		step := speed * interval / math.Hypot(radiusX*math.Sin(angle), radiusY*math.Cos(angle)+90*math.Cos(angle*3))
		if math.IsNaN(step) || step <= 0 || step > 0.05 {
			step = 0.002
		}
		angle += step
		time += interval
		if !complete && angle > 3.2 {
			break
		}
		if time > 400 {
			break
		}
	}
	_ = random
	return samples, round(time, 3)
}

func round(value float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}

func writeJSON(path string, value any) {
	data, err := json.Marshal(value)
	must(err)
	must(os.MkdirAll(filepath.Dir(path), 0o755))
	must(os.WriteFile(path, data, 0o644))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
