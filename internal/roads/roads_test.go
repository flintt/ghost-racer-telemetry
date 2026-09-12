package roads

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// writeArchive builds a miniature level archive shaped like the real thing.
func writeArchive(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	levels := filepath.Join(dir, "content", "levels")
	if err := os.MkdirAll(levels, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(levels, "East_Coast_USA.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	entry, err := writer.Create("levels/east_coast_usa/main/MissionGroup/ai_roads/items.level.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if _, err := entry.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	// A second group, to prove every items file is read, not just the first.
	other, err := writer.Create("levels/east_coast_usa/main/MissionGroup/AIWaypointsGroup/items.level.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Write([]byte(`{"class":"BeamNGWaypoint","name":"wp1"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

const visibleRoad = `{"class":"DecalRoad","persistentId":"a","__parent":"ai_roads","material":"road_asphalt_2lane","drivability":1,"nodes":[[0,0,10,8],[100,0,10,8],[200,50,12,10]]}`
const invisibleRoad = `{"class":"DecalRoad","persistentId":"b","__parent":"ai_roads","material":"road_invisible","drivability":0.4,"nodes":[[0,500,10,3.5],[100,500,10,3.5]]}`
const faraway = `{"class":"DecalRoad","persistentId":"c","__parent":"ai_roads","material":"road_asphalt_2lane","drivability":1,"nodes":[[9000,9000,0,6],[9100,9000,0,6]]}`
const notARoad = `{"class":"TSStatic","shapeName":"tree.dae","position":[1,2,3]}`

func TestExtractReadsRoadsAndWidths(t *testing.T) {
	root := writeArchive(t, visibleRoad, invisibleRoad, faraway, notARoad)
	// The archive is named East_Coast_USA.zip while the level id is lowercase.
	archive, err := FindArchive(root, "east_coast_usa")
	if err != nil {
		t.Fatal(err)
	}
	level, err := Extract(archive, "east_coast_usa")
	if err != nil {
		t.Fatal(err)
	}

	if len(level.Roads) != 3 {
		t.Fatalf("extracted %d roads, want 3", len(level.Roads))
	}
	if level.NodeCount != 7 {
		t.Errorf("node count = %d, want 7", level.NodeCount)
	}
	first := level.Roads[0]
	if first.Material != "road_asphalt_2lane" || first.Invisible {
		t.Errorf("first road = %+v", first)
	}
	// Width travels with the node, which is what makes an edge possible.
	if first.Nodes[0][3] != 8 || first.Nodes[2][3] != 10 {
		t.Errorf("node widths = %v", first.Nodes)
	}
	if first.Bounds != [4]float64{0, 0, 200, 50} {
		t.Errorf("bounds = %v", first.Bounds)
	}
	if !level.Roads[1].Invisible {
		t.Errorf("an invisible material must be flagged: %+v", level.Roads[1])
	}
}

func TestClipDropsDistantAndInvisibleRoads(t *testing.T) {
	root := writeArchive(t, visibleRoad, invisibleRoad, faraway, notARoad)
	archive, _ := FindArchive(root, "east_coast_usa")
	level, err := Extract(archive, "east_coast_usa")
	if err != nil {
		t.Fatal(err)
	}

	near := level.Clip(-50, -50, 250, 100, false)
	if len(near) != 1 || near[0].Material != "road_asphalt_2lane" {
		t.Fatalf("clip returned %d roads: %+v", len(near), near)
	}
	withAI := level.Clip(-50, -50, 250, 600, true)
	if len(withAI) != 2 {
		t.Errorf("including the AI network should give 2 roads, got %d", len(withAI))
	}
}

func TestStoreCachesExtraction(t *testing.T) {
	root := writeArchive(t, visibleRoad)
	cache := t.TempDir()
	store := NewStore(cache)

	first, err := store.Load(root, "east_coast_usa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "east_coast_usa.json")); err != nil {
		t.Fatalf("cache file was not written: %v", err)
	}
	if len(first.Roads) != 1 {
		t.Errorf("extracted %d roads", len(first.Roads))
	}
	// A cached extraction outlives the archive: the game does not have to be
	// installed to look at roads that were already read out of it.
	if err := os.Remove(filepath.Join(root, "content", "levels", "East_Coast_USA.zip")); err != nil {
		t.Fatal(err)
	}
	again, err := NewStore(cache).Load(root, "east_coast_usa")
	if err != nil {
		t.Fatalf("cache should answer without the archive: %v", err)
	}
	if len(again.Roads) != 1 {
		t.Errorf("cached level has %d roads", len(again.Roads))
	}
}
