package ghost

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeFixture lays down the minimum tree the mod produces: a registry, a
// library manifest and one sample file.
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "freeRoam", "east_coast_usa", "starts", "s001")
	if err := os.MkdirAll(filepath.Join(base, "ghostracer.save.ghosts"), 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(path string, value any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(filepath.Join(root, "freeRoam", "east_coast_usa", "startLines.json"), map[string]any{
		"formatVersion": 3,
		"level":         "east_coast_usa",
		"nextId":        2,
		"lines": []any{map[string]any{
			"id": "s001", "name": "Coast loop", "userNamed": true, "startKey": "s001",
			"position": []float64{10, 20, 30}, "normal": []float64{0, 1, 0},
			"finishPosition": []float64{90, 20, 30}, "finishNormal": []float64{1, 0, 0},
		}},
	})

	write(filepath.Join(base, "ghostracer.save.library.json"), map[string]any{
		"formatVersion": 1,
		"nextId":        4,
		"ghosts": []any{
			map[string]any{
				"id": "g000001", "label": "Lap 1", "lapTime": 42.5, "duration": 42.5, "sampleInterval": 0.02,
				"complete": true, "vehicle": "sunburst", "color": "orange", "hasInputs": true,
				"file": "ghostReplays/freeRoam/east_coast_usa/starts/s001/ghostracer.save.ghosts/g000001.json",
			},
			map[string]any{
				"id": "g000002", "label": "Lap 2", "lapTime": 41.0, "complete": true, "vehicle": "etk800",
				"file": "ghostReplays/freeRoam/east_coast_usa/starts/s001/ghostracer.save.ghosts/g000002.json",
			},
			map[string]any{
				"id": "g000003", "label": "Manual", "manual": true, "source": "manual", "complete": true,
				"file": "ghostReplays/freeRoam/east_coast_usa/starts/s001/ghostracer.save.ghosts/g000003.json",
			},
			map[string]any{
				"id": 4, "label": "Partial", "complete": false, "incompleteReason": "reset",
				"file": "ghostReplays/freeRoam/east_coast_usa/starts/s001/ghostracer.save.ghosts/4.json",
			},
		},
	})

	// Two seconds of a straight-line run at 10 m/s, with inputs.
	samples := [][]float64{}
	for i := 0; i < 101; i++ {
		time := float64(i) * 0.02
		samples = append(samples, []float64{
			time, 10 * time, 0, 5, 1, 0, 0, 0, 0, 1, 10, 1, 0, 3, 0, 0,
		})
	}
	write(filepath.Join(base, "ghostracer.save.ghosts", "g000001.json"), map[string]any{
		"formatVersion": 2, "sampleInterval": 0.02, "duration": 2.0, "lapTime": 42.5,
		"vehicle": "sunburst", "complete": true, "samples": samples,
		"startLine": map[string]any{"level": "east_coast_usa", "position": []float64{10, 20, 30},
			"normal": []float64{0, 1, 0}, "halfWidth": 8},
	})

	// A pre-2.0 replay: object rows and no explicit timestamps.
	legacyRows := []any{}
	for i := 0; i < 10; i++ {
		legacyRows = append(legacyRows, map[string]any{
			"pos":      map[string]float64{"x": float64(i), "y": 0, "z": 5},
			"dirFront": map[string]float64{"x": 1, "y": 0, "z": 0},
			"dirUp":    map[string]float64{"x": 0, "y": 0, "z": 1},
			"speed":    20.0,
		})
	}
	write(filepath.Join(base, "ghostracer.save.ghosts", "g000002.json"), map[string]any{
		"formatVersion": 1, "sampleInterval": 0.05, "samples": legacyRows,
	})

	return root
}

func TestScanFindsRegisteredLibrary(t *testing.T) {
	root := writeFixture(t)
	scanner := NewScanner([]Root{{Name: "game", Path: root}})
	catalog := scanner.Scan()

	if len(catalog.Libraries) != 1 {
		t.Fatalf("expected 1 library, got %d", len(catalog.Libraries))
	}
	library := catalog.Libraries[0]
	if library.StartName != "Coast loop" {
		t.Errorf("start name = %q, want the registry name", library.StartName)
	}
	if !library.Registered || !library.PointToPoint {
		t.Errorf("registry overlay missing: registered=%v p2p=%v", library.Registered, library.PointToPoint)
	}
	if library.Level != "east_coast_usa" || library.Kind != "freeRoam" {
		t.Errorf("classification = %s/%s", library.Level, library.Kind)
	}
	// Counts follow the mod's categories: incomplete, then manual, then lap.
	if library.LapCount != 4 || library.CompleteCount != 2 || library.ManualCount != 1 || library.IncompleteCount != 1 {
		t.Errorf("counts = total %d complete %d manual %d incomplete %d",
			library.LapCount, library.CompleteCount, library.ManualCount, library.IncompleteCount)
	}
	if library.BestLapTime == nil || *library.BestLapTime != 41.0 {
		t.Errorf("best lap = %v, want 41", library.BestLapTime)
	}
	if _, ok := scanner.Lookup(library.Key); !ok {
		t.Errorf("library key %q is not resolvable", library.Key)
	}
}

func TestListLapsRanksTimedLapsOnly(t *testing.T) {
	root := writeFixture(t)
	scanner := NewScanner([]Root{{Name: "game", Path: root}})
	library := scanner.Scan().Libraries[0]

	laps, err := ListLaps(root, library)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]LapMeta{}
	for _, lap := range laps {
		byID[lap.ID] = lap
	}
	if byID["g000002"].Rank != 1 || byID["g000001"].Rank != 2 {
		t.Errorf("ranks = lap2 %d lap1 %d, want the faster lap first",
			byID["g000002"].Rank, byID["g000001"].Rank)
	}
	// Lap 4 keeps a bare numeric id on purpose: pre-2.x and hand-made manifests
	// carry one, and it must still decode.
	if byID["4"].ID != "4" {
		t.Errorf("numeric id decoded as %q", byID["4"].ID)
	}
	if byID["g000003"].Rank != 0 || byID["4"].Rank != 0 {
		t.Errorf("manual and incomplete laps must stay unranked, got %d and %d", byID["g000003"].Rank, byID["4"].Rank)
	}
	if byID["g000003"].Category != "manual" || byID["4"].Category != "incomplete" {
		t.Errorf("categories = %q and %q", byID["g000003"].Category, byID["4"].Category)
	}
}

func TestLoadLapDerivesChannels(t *testing.T) {
	root := writeFixture(t)
	scanner := NewScanner([]Root{{Name: "game", Path: root}})
	library := scanner.Scan().Libraries[0]

	lap, err := LoadLap(root, library, "g000001")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(lap.Channels.T); got != 101 {
		t.Fatalf("sample count = %d, want 101", got)
	}
	if !lap.HasInputs || len(lap.Channels.Throttle) != 101 {
		t.Errorf("driver inputs were not decoded")
	}
	// 10 m/s for two seconds is 20 m of travel.
	if math.Abs(lap.Summary.Distance-20) > 0.01 {
		t.Errorf("distance = %.3f, want 20", lap.Summary.Distance)
	}
	if math.Abs(lap.Summary.TopSpeed-10) > 0.001 {
		t.Errorf("top speed = %.3f, want 10", lap.Summary.TopSpeed)
	}
	if lap.Summary.FullThrottlePct != 100 {
		t.Errorf("full throttle share = %.1f, want 100", lap.Summary.FullThrottlePct)
	}
	if lap.StartLine == nil || lap.StartLine.HalfWidth != 8 {
		t.Errorf("start line was not carried through: %+v", lap.StartLine)
	}
}

func TestLoadLapAcceptsLegacyObjectRows(t *testing.T) {
	root := writeFixture(t)
	scanner := NewScanner([]Root{{Name: "game", Path: root}})
	library := scanner.Scan().Libraries[0]

	lap, err := LoadLap(root, library, "g000002")
	if err != nil {
		t.Fatal(err)
	}
	if len(lap.Channels.T) != 10 {
		t.Fatalf("legacy rows decoded to %d samples", len(lap.Channels.T))
	}
	if lap.HasInputs {
		t.Errorf("a 1.6 replay carries no driver inputs")
	}
	// Timestamps are synthesized from the sample interval.
	if math.Abs(lap.Channels.T[4]-0.2) > 1e-9 {
		t.Errorf("t[4] = %v, want 0.2", lap.Channels.T[4])
	}
}

func TestImportWritesLibraryAndMerges(t *testing.T) {
	dataRoot := t.TempDir()
	root := Root{Name: "import", Path: dataRoot, Writable: true}
	payload := &ImportPayload{
		Source:  "ghost-racer-enhanced/2.20.3",
		Level:   "east_coast_usa",
		StartID: "s001",
		Laps: []ImportLap{{
			ID:      "g000007",
			Label:   "Exported lap",
			Vehicle: "sunburst",
			Samples: RawSamples{
				json.RawMessage(`[0,0,0,5,1,0,0,0,0,1,10]`),
				json.RawMessage(`[0.02,0.2,0,5,1,0,0,0,0,1,10]`),
			},
		}},
	}

	result, err := Import(root, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 1 || result.Written[0] != "g000007" {
		t.Fatalf("import wrote %v", result.Written)
	}

	scanner := NewScanner([]Root{root})
	catalog := scanner.Scan()
	if len(catalog.Libraries) != 1 || catalog.Libraries[0].LapCount != 1 {
		t.Fatalf("imported library did not show up: %+v", catalog.Libraries)
	}

	// A second import of the same id replaces it rather than duplicating.
	if _, err := Import(root, payload); err != nil {
		t.Fatal(err)
	}
	if laps, err := ListLaps(dataRoot, scanner.Scan().Libraries[0]); err != nil || len(laps) != 1 {
		t.Fatalf("re-import produced %d laps (err %v)", len(laps), err)
	}
}

func TestDeleteLapRemovesEntryAndFile(t *testing.T) {
	root := writeFixture(t)
	scanner := NewScanner([]Root{{Name: "game", Path: root}})
	library := scanner.Scan().Libraries[0]

	if err := DeleteLap(root, library, "g000001"); err != nil {
		t.Fatal(err)
	}
	laps, err := ListLaps(root, library)
	if err != nil {
		t.Fatal(err)
	}
	for _, lap := range laps {
		if lap.ID == "g000001" {
			t.Fatalf("lap 1 is still in the manifest")
		}
	}
	sample := filepath.Join(root, "freeRoam", "east_coast_usa", "starts", "s001",
		"ghostracer.save.ghosts", "g000001.json")
	if _, err := os.Stat(sample); !os.IsNotExist(err) {
		t.Errorf("sample file still exists: %v", err)
	}
}

func TestResolveSamplePathRejectsEscape(t *testing.T) {
	root := writeFixture(t)
	library := &Library{Rel: "freeRoam/east_coast_usa/starts/s001/ghostracer.save.json"}
	meta := &LapMeta{ID: "99", File: "ghostReplays/../../../etc/passwd"}
	if _, err := resolveSamplePath(root, library, meta); err == nil {
		t.Fatal("a path escaping the root must not resolve")
	}
}

// writeSlopeFixture lays down one lap climbing a known, constant slope.
func writeSlopeFixture(t *testing.T, percent float64) (string, *Library) {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "freeRoam", "hill", "starts", "s001")
	if err := os.MkdirAll(filepath.Join(base, "ghostracer.save.ghosts"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string, value any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 200 m of travel at 10 m/s, rising `percent` metres per 100 m of ground.
	samples := [][]float64{}
	for i := 0; i <= 1000; i++ {
		time := float64(i) * 0.02
		along := 10 * time
		samples = append(samples, []float64{
			time, along, 0, 100 + along*percent/100, 1, 0, 0, 0, 0, 1, 10,
		})
	}
	write(filepath.Join(base, "ghostracer.save.ghosts", "g000001.json"), map[string]any{
		"formatVersion": 2, "sampleInterval": 0.02, "lapTime": 20.0,
		"vehicle": "sunburst", "complete": true, "samples": samples,
	})
	write(filepath.Join(base, "ghostracer.save.library.json"), map[string]any{
		"formatVersion": 1, "nextId": 2,
		"ghosts": []any{map[string]any{
			"id": "g000001", "label": "Climb", "lapTime": 20.0, "complete": true,
			"file": "ghostReplays/freeRoam/hill/starts/s001/ghostracer.save.ghosts/g000001.json",
		}},
	})
	scanner := NewScanner([]Root{{Name: "game", Path: root}})
	return root, scanner.Scan().Libraries[0]
}

func TestGradientMatchesTheSlope(t *testing.T) {
	const percent = 8.0
	root, library := writeSlopeFixture(t, percent)
	lap, err := LoadLap(root, library, "g000001")
	if err != nil {
		t.Fatal(err)
	}

	// Away from the ends, where the window is one-sided, every sample should
	// report the slope it was built with.
	for _, index := range []int{200, 500, 800} {
		if math.Abs(lap.Channels.Gradient[index]-percent) > 0.05 {
			t.Errorf("gradient at %d = %.3f%%, want %g%%", index, lap.Channels.Gradient[index], percent)
		}
	}
	if math.Abs(lap.Summary.MaxGradient-percent) > 0.05 {
		t.Errorf("max gradient = %.3f%%, want %g%%", lap.Summary.MaxGradient, percent)
	}
	// A climb of 8 m per 100 m over 200 m of ground is 16 m, and the travelled
	// distance is the hypotenuse rather than the ground run.
	if math.Abs(lap.Summary.ElevationGain-16) > 0.2 {
		t.Errorf("climb = %.2f m, want 16", lap.Summary.ElevationGain)
	}
	if lap.Summary.Distance <= 200 {
		t.Errorf("distance %.2f m should exceed the 200 m ground run", lap.Summary.Distance)
	}
}
