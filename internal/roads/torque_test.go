package roads

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

const torquePrefab = `//--- OBJECT WRITE BEGIN ---
new SimGroup(bridge_prefab) {
   canSave = "1";
   canSaveDynamicFields = "1";

   new DecalRoad(bridge_deck) {
      Material = "road_asphalt_2lane";
      textureLength = "5";
      breakAngle = "3";
      renderPriority = "10";
      position = "10 20 30";
      drivability = "1";
      Node = "10 20 30 9";
      Node = "60 20 30 9";
      Node = "110 20 30 9";
   };
   new TSStatic(pillar) {
      shapeName = "pillar.dae";
      position = "50 20 10";
   };
   new MeshRoad(span) {
      Material = "bridge_concrete";
      Node = "120 20 30 12 1.5 0 0 1";
      Node = "200 20 30 12 1.5 0 0 1";
   };
};
//--- OBJECT WRITE END ---
`

func TestTorquePrefabRoads(t *testing.T) {
	level := &Level{Files: map[string]int{}, Bounds: [4]float64{inf(1), inf(1), inf(-1), inf(-1)}}
	found := readTorque([]byte(torquePrefab), level)
	if found != 2 {
		t.Fatalf("read %d roads, want the deck and the span", found)
	}

	byMaterial := map[string]*Road{}
	for _, road := range level.Roads {
		byMaterial[road.Material] = road
	}
	deck := byMaterial["road_asphalt_2lane"]
	if deck == nil {
		t.Fatal("the DecalRoad deck was not read")
	}
	if deck.Class != "DecalRoad" || deck.Drivability != 1 || len(deck.Nodes) != 3 {
		t.Errorf("deck = %+v", deck)
	}
	if deck.Nodes[0] != (Node{10, 20, 30, 9}) {
		t.Errorf("deck nodes = %v", deck.Nodes)
	}
	if deck.Bounds != [4]float64{10, 20, 110, 20} {
		t.Errorf("deck bounds = %v", deck.Bounds)
	}

	span := byMaterial["bridge_concrete"]
	if span == nil {
		t.Fatal("the MeshRoad span was not read")
	}
	// A MeshRoad node carries depth and a normal after the width; only the first
	// four numbers are wanted.
	if span.Class != "MeshRoad" || len(span.Nodes) != 2 || span.Nodes[0][3] != 12 {
		t.Errorf("span = %+v", span)
	}
	// A MeshRoad without drivability still counts as road, which the filter allows.
	if span.Drivability != 0 {
		t.Errorf("span drivability = %v, want 0", span.Drivability)
	}
}

func TestTorquePrefabInsideArchive(t *testing.T) {
	dir := t.TempDir()
	levels := filepath.Join(dir, "content", "levels")
	if err := os.MkdirAll(levels, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(levels, "hirochi_raceway.zip"))
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	items, _ := writer.Create("levels/hirochi_raceway/main/MissionGroup/ai_roads/items.level.json")
	items.Write([]byte(visibleRoad + "\n"))
	prefab, _ := writer.Create("levels/hirochi_raceway/art/shapes/bridge.prefab")
	prefab.Write([]byte(torquePrefab))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()

	source, err := Locate(dir, "", "hirochi_raceway")
	if err != nil {
		t.Fatal(err)
	}
	level, err := Extract(source, "hirochi_raceway")
	if err != nil {
		t.Fatal(err)
	}
	if level.Files["prefabTorque"] != 1 || level.Files["prefabRoads"] != 2 {
		t.Errorf("file report = %v", level.Files)
	}
	// The bridge must survive the filter the map uses.
	kept := level.Clip(-1000, -1000, 1000, 1000, DefaultFilter())
	bridge := false
	for _, road := range kept {
		if road.Material == "bridge_concrete" {
			bridge = true
		}
	}
	if !bridge {
		t.Errorf("the prefab bridge was filtered out of %d kept roads", len(kept))
	}
}
