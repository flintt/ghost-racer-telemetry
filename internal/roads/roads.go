// Package roads extracts road geometry from BeamNG level archives, so a lap can
// be drawn against the road it was driven on.
//
// A level ships as content/levels/<level>.zip, and its objects live in
// newline-delimited JSON inside it:
//
//	levels/<level>/main/MissionGroup/<group>/items.level.json
//
// Each road is one object:
//
//	{"class":"DecalRoad","material":"road_asphalt_2lane","drivability":1,
//	 "__parent":"ai_roads","nodes":[[x,y,z,width], ...]}
//
// Every node carries its own width, so the road edges are the centre line offset
// by half that width along the normal — which is what makes the boundary real
// rather than a guess.
package roads

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Node is one centre-line point: x, y, z and the road's width there.
type Node [4]float64

// Road is one DecalRoad from a level.
type Road struct {
	Material    string     `json:"material"`
	Group       string     `json:"group"`
	Drivability float64    `json:"drivability"`
	Invisible   bool       `json:"invisible"`
	Nodes       []Node     `json:"nodes"`
	Bounds      [4]float64 `json:"bounds"` // minX, minY, maxX, maxY
}

// Level is every road extracted from one level.
type Level struct {
	Level     string     `json:"level"`
	Source    string     `json:"source"`
	Roads     []*Road    `json:"roads"`
	Bounds    [4]float64 `json:"bounds"`
	NodeCount int        `json:"nodeCount"`
}

// item is the shape we care about in an items.level.json line.
type item struct {
	Class       string          `json:"class"`
	Material    string          `json:"material"`
	Parent      string          `json:"__parent"`
	Drivability float64         `json:"drivability"`
	Nodes       json.RawMessage `json:"nodes"`
}

// FindArchive locates a level archive, tolerating the case differences between
// a level's id and its file name (east_coast_usa.zip, but Cliff.zip).
func FindArchive(gameRoot, level string) (string, error) {
	dir := filepath.Join(gameRoot, "content", "levels")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	want := strings.ToLower(level) + ".zip"
	for _, entry := range entries {
		if strings.ToLower(entry.Name()) == want {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("no archive for level %q under %s", level, dir)
}

// Extract reads every road out of a level archive. Only the small
// items.level.json entries are decompressed; the textures and meshes that make
// up most of a 900 MB archive are never touched.
func Extract(archivePath, level string) (*Level, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	result := &Level{
		Level:  level,
		Source: archivePath,
		Bounds: [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)},
	}
	for _, file := range reader.File {
		if !strings.HasSuffix(file.Name, "items.level.json") || file.UncompressedSize64 == 0 {
			continue
		}
		if err := readItems(file, result); err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, err)
		}
	}
	if len(result.Roads) == 0 {
		return nil, fmt.Errorf("no roads found in %s", archivePath)
	}
	return result, nil
}

func readItems(file *zip.File, into *Level) error {
	stream, err := file.Open()
	if err != nil {
		return err
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	// Object lines carry whole roads and can be long.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) < 2 || line[0] != '{' {
			continue
		}
		// Cheap reject before spending a JSON parse on 2 000 other objects.
		if !strings.Contains(line, `"DecalRoad"`) {
			continue
		}
		var parsed item
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			continue
		}
		if parsed.Class != "DecalRoad" || len(parsed.Nodes) == 0 {
			continue
		}
		road := buildRoad(&parsed)
		if road == nil {
			continue
		}
		into.Roads = append(into.Roads, road)
		into.NodeCount += len(road.Nodes)
		into.Bounds[0] = math.Min(into.Bounds[0], road.Bounds[0])
		into.Bounds[1] = math.Min(into.Bounds[1], road.Bounds[1])
		into.Bounds[2] = math.Max(into.Bounds[2], road.Bounds[2])
		into.Bounds[3] = math.Max(into.Bounds[3], road.Bounds[3])
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func buildRoad(parsed *item) *Road {
	var raw [][]float64
	if err := json.Unmarshal(parsed.Nodes, &raw); err != nil {
		return nil
	}
	road := &Road{
		Material:    parsed.Material,
		Group:       parsed.Parent,
		Drivability: parsed.Drivability,
		// The AI network is laid out as roads with an invisible material; they
		// are drivable but they are not the road surface a driver sees.
		Invisible: strings.Contains(strings.ToLower(parsed.Material), "invisible"),
		Bounds:    [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)},
	}
	for _, node := range raw {
		if len(node) < 4 {
			continue
		}
		point := Node{round(node[0], 2), round(node[1], 2), round(node[2], 2), round(node[3], 2)}
		road.Nodes = append(road.Nodes, point)
		road.Bounds[0] = math.Min(road.Bounds[0], point[0])
		road.Bounds[1] = math.Min(road.Bounds[1], point[1])
		road.Bounds[2] = math.Max(road.Bounds[2], point[0])
		road.Bounds[3] = math.Max(road.Bounds[3], point[1])
	}
	if len(road.Nodes) < 2 {
		return nil
	}
	return road
}

// Clip returns the roads overlapping a box, which is how a lap asks for just
// the roads it was driven on instead of a whole county.
func (level *Level) Clip(minX, minY, maxX, maxY float64, includeInvisible bool) []*Road {
	out := make([]*Road, 0, 64)
	for _, road := range level.Roads {
		if road.Invisible && !includeInvisible {
			continue
		}
		if road.Bounds[0] > maxX || road.Bounds[2] < minX ||
			road.Bounds[1] > maxY || road.Bounds[3] < minY {
			continue
		}
		out = append(out, road)
	}
	return out
}

func round(value float64, digits int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}
