// Command genfixture writes a synthetic ghostReplays tree so the server and UI
// can be exercised without a BeamNG install.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

func main() {
	out := flag.String("out", "testdata/ghostReplays", "directory to write the fixture into")
	size := flag.Float64("size", 1, "circuit size multiplier; raises the sample count per lap")
	relief := flag.Float64("relief", 1, "elevation amplitude multiplier, for exercising gradient and the tilted view")
	gameOut := flag.String("game-out", "", "also write a fake BeamNG install whose level archive carries roads along the generated track")
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
			// Each lap is stronger in different parts of the circuit, so the
			// sector analysis has something real to find.
			phase := float64(lap) * 1.7
			samples, lapTime := generateLap(random, pace, complete, start.seed, *size, phase, *relief)

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

	if *gameOut != "" {
		writeLevelArchive(*gameOut, level, *size, *relief)
		fmt.Printf("fake level archive written to %s\n", *gameOut)
	}
	fmt.Printf("fixture written to %s\n", *out)
}

// writeLevelArchive fakes a BeamNG install: one level archive whose drivable
// roads follow the generated track, so the road overlay can be exercised without
// the game installed.
func writeLevelArchive(root, level string, size, relief float64) {
	dir := filepath.Join(root, "content", "levels")
	must(os.MkdirAll(dir, 0o755))
	file, err := os.Create(filepath.Join(dir, level+".zip"))
	must(err)
	defer file.Close()

	archive := zip.NewWriter(file)
	entry, err := archive.Create(
		"levels/" + level + "/main/MissionGroup/ai_roads/items.level.json")
	must(err)

	centre := func(angle float64) [3]float64 {
		return [3]float64{
			220 * size * math.Cos(angle),
			130*size*math.Sin(angle) + 30*size*math.Sin(angle*3),
			12 + 6*relief*math.Sin(angle*2),
		}
	}
	// Several road objects meeting end to end, as a real level stores them.
	const segments, per = 6, 40
	for s := 0; s < segments; s++ {
		nodes := [][]float64{}
		for k := 0; k <= per; k++ {
			angle := (float64(s) + float64(k)/per) * (2 * math.Pi / segments)
			point := centre(angle)
			nodes = append(nodes, []float64{
				round(point[0], 3), round(point[1], 3), round(point[2], 3),
				round(9+2*math.Sin(angle*2), 2),
			})
		}
		writeJSONLine(entry, map[string]any{
			"class": "DecalRoad", "persistentId": fmt.Sprintf("ai%d", s),
			"__parent": "ai_roads", "material": "road_invisible",
			"drivability": 1, "nodes": nodes,
		})
	}
	// Decals with no drivability, which the overlay must leave out.
	for i := 0; i < 20; i++ {
		point := centre(float64(i) * 2 * math.Pi / 20)
		writeJSONLine(entry, map[string]any{
			"class": "DecalRoad", "persistentId": fmt.Sprintf("decal%d", i),
			"__parent": "Decal_roads", "material": "m_dirt_variation_01",
			"nodes": [][]float64{
				{point[0] + 20, point[1] + 20, point[2], 6},
				{point[0] + 34, point[1] + 29, point[2], 6},
			},
		})
	}
	must(archive.Close())
}

func writeJSONLine(writer io.Writer, value any) {
	data, err := json.Marshal(value)
	must(err)
	_, err = writer.Write(append(data, '\n'))
	must(err)
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
func generateLap(random *rand.Rand, pace float64, complete bool, seed int64, size float64, phase float64, relief float64) ([][]float64, float64) {
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
		// A per-lap strength profile around the circuit: nobody is quickest
		// everywhere.
		targetSpeed *= 1 + 0.06*math.Sin(angle*2+phase)
		wobble := 1 + 0.02*math.Sin(angle*7+float64(seed))
		speed := targetSpeed * wobble

		x := radiusX * math.Cos(angle)
		y := radiusY*math.Sin(angle) + 30*math.Sin(angle*3)
		z := 12 + 6*relief*math.Sin(angle*2)

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
