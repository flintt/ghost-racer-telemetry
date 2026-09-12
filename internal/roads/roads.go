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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Node is one centre-line point: x, y, z and the road's width there.
type Node [4]float64

// Road is one road-like object from a level: a DecalRoad, or a MeshRoad, which
// is what bridges and elevated sections are built from.
type Road struct {
	Class       string     `json:"class"`
	Material    string     `json:"material"`
	Group       string     `json:"group"`
	Drivability float64    `json:"drivability"`
	Invisible   bool       `json:"invisible"`
	Nodes       []Node     `json:"nodes"`
	Bounds      [4]float64 `json:"bounds"` // minX, minY, maxX, maxY
}

// Level is every road extracted from one level.
type Level struct {
	Level  string `json:"level"`
	Source string `json:"source"`
	// Files says what was actually read, keyed by kind. Whether a level even has
	// prefab files is otherwise impossible to tell from the outside.
	Files     map[string]int `json:"files"`
	Roads     []*Road        `json:"roads"`
	Bounds    [4]float64     `json:"bounds"`
	NodeCount int            `json:"nodeCount"`
}

// item is the shape we care about in an items.level.json line.
type item struct {
	Class       string          `json:"class"`
	Material    string          `json:"material"`
	Parent      string          `json:"__parent"`
	Drivability float64         `json:"drivability"`
	Nodes       json.RawMessage `json:"nodes"`
}

// Source is one place a level's objects can be read from: an archive, or an
// unpacked directory. Mods ship both ways.
type Source struct {
	Description string
	files       []itemsFile
	// prefabCandidates counts old-style .prefab files, which are Torque text and
	// not JSON. If a level's bridges live in those, nothing here can read them
	// and the count is what says so.
	prefabCandidates int
	closer           func() error
}

type itemsFile struct {
	name string
	open func() (io.ReadCloser, error)
}

// Locate finds a level. Official levels are archives under the install's
// content/levels; mods live in the user folder, either as archives under mods/
// or unpacked into a directory, and a mod archive holds its level under the
// same levels/<name>/ prefix as an official one.
func Locate(gameRoot, userRoot, level string) (*Source, error) {
	tried := []string{}

	if gameRoot != "" {
		dir := filepath.Join(gameRoot, "content", "levels")
		tried = append(tried, dir)
		if source := findInDirectory(dir, level, false); source != nil {
			return source, nil
		}
	}
	if userRoot != "" {
		// Mod archives sit directly in mods/ and in its subfolders (repo/ holds
		// everything installed from the repository).
		mods := filepath.Join(userRoot, "mods")
		tried = append(tried, mods)
		if source := findInDirectory(mods, level, true); source != nil {
			return source, nil
		}
	}
	return nil, fmt.Errorf("no level %q under %s", level, strings.Join(tried, ", "))
}

// findInDirectory looks for an archive or an unpacked tree holding the level.
func findInDirectory(dir, level string, recurse bool) *Source {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	want := strings.ToLower(level)

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			// content/levels/<level>/ or mods/unpacked/<mod>/levels/<level>/
			if strings.ToLower(entry.Name()) == want {
				if source := openDirectory(path, want); source != nil {
					return source
				}
			}
			if source := openDirectory(filepath.Join(path, "levels", entry.Name()), want); source != nil {
				return source
			}
			if unpacked := findUnpackedLevel(path, want); unpacked != nil {
				return unpacked
			}
			if recurse {
				if source := findInDirectory(path, level, false); source != nil {
					return source
				}
			}
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
			continue
		}
		if source := openArchive(path, want); source != nil {
			return source
		}
	}
	return nil
}

func findUnpackedLevel(modDir, level string) *Source {
	return openDirectory(filepath.Join(modDir, "levels", level), level)
}

// carriesObjects says whether a file can hold level objects. Roads live in the
// per-group items files, but a bridge is very often a prefab, whose objects sit
// in a file of their own and would otherwise never be read.
func fileKind(name string) string {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".prefab.json") {
		return "prefabJSON"
	}
	return "items"
}

func carriesObjects(lowerName string) bool {
	return strings.HasSuffix(lowerName, "items.level.json") ||
		strings.HasSuffix(lowerName, ".prefab.json")
}

// levelPrefix is where a level's objects live inside an archive or a mod tree.
func levelPrefix(level string) string {
	return "levels/" + level + "/"
}

func openArchive(path, level string) *Source {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	prefix := levelPrefix(level)
	source := &Source{Description: path, closer: reader.Close}
	for _, file := range reader.File {
		name := strings.ToLower(filepath.ToSlash(file.Name))
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if strings.HasSuffix(name, ".prefab") {
			source.prefabCandidates++
			continue
		}
		if !carriesObjects(name) {
			continue
		}
		if file.UncompressedSize64 == 0 {
			continue
		}
		entry := file
		source.files = append(source.files, itemsFile{
			name: file.Name,
			open: func() (io.ReadCloser, error) { return entry.Open() },
		})
	}
	if len(source.files) == 0 {
		reader.Close()
		return nil
	}
	return source
}

// openDirectory reads an unpacked level: the same layout, on disk.
func openDirectory(root, level string) *Source {
	stat, err := os.Stat(root)
	if err != nil || !stat.IsDir() {
		return nil
	}
	source := &Source{Description: root, closer: func() error { return nil }}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if !carriesObjects(strings.ToLower(entry.Name())) {
			return nil
		}
		name := path
		source.files = append(source.files, itemsFile{
			name: name,
			open: func() (io.ReadCloser, error) { return os.Open(name) },
		})
		return nil
	})
	_ = level
	if len(source.files) == 0 {
		return nil
	}
	return source
}

// Extract reads every road out of a located level. Only the small
// items.level.json entries are read; the textures and meshes that make up most
// of a 900 MB archive are never touched.
func Extract(source *Source, level string) (*Level, error) {
	defer func() {
		if source.closer != nil {
			_ = source.closer()
		}
	}()

	result := &Level{
		Level:  level,
		Source: source.Description,
		Files:  map[string]int{},
		Bounds: [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)},
	}
	for _, file := range source.files {
		result.Files[fileKind(file.name)]++
		if err := readItems(file, result); err != nil {
			return nil, fmt.Errorf("%s: %w", file.name, err)
		}
	}
	result.Files["prefabObjects"] = source.prefabCandidates
	if len(result.Roads) == 0 {
		return nil, fmt.Errorf("no roads found in %s", source.Description)
	}
	return result, nil
}

func readItems(file itemsFile, into *Level) error {
	stream, err := file.open()
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(stream, 64<<20))
	stream.Close()
	if err != nil {
		return err
	}
	if !bytes.Contains(data, []byte(`"DecalRoad"`)) && !bytes.Contains(data, []byte(`"MeshRoad"`)) {
		return nil
	}

	found := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) < 2 || trimmed[0] != '{' {
			continue
		}
		if road := roadFromJSON(trimmed); road != nil {
			addRoad(into, road)
			found++
		}
	}
	if found > 0 {
		return nil
	}

	// Prefabs are written as one pretty-printed document rather than a line per
	// object, so walk the whole thing for anything shaped like a road.
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil
	}
	walkForRoads(document, into)
	return nil
}

func roadFromJSON(raw []byte) *Road {
	var parsed item
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	if (parsed.Class != "DecalRoad" && parsed.Class != "MeshRoad") || len(parsed.Nodes) == 0 {
		return nil
	}
	return buildRoad(&parsed)
}

// walkForRoads descends any JSON shape looking for road objects, because a
// prefab can nest them under keys this package has no business knowing about.
func walkForRoads(node any, into *Level) {
	switch value := node.(type) {
	case map[string]any:
		if class, ok := value["class"].(string); ok && (class == "DecalRoad" || class == "MeshRoad") {
			if encoded, err := json.Marshal(value); err == nil {
				if road := roadFromJSON(encoded); road != nil {
					addRoad(into, road)
					return
				}
			}
		}
		for _, child := range value {
			walkForRoads(child, into)
		}
	case []any:
		for _, child := range value {
			walkForRoads(child, into)
		}
	}
}

func addRoad(into *Level, road *Road) {
	into.Roads = append(into.Roads, road)
	into.NodeCount += len(road.Nodes)
	into.Bounds[0] = math.Min(into.Bounds[0], road.Bounds[0])
	into.Bounds[1] = math.Min(into.Bounds[1], road.Bounds[1])
	into.Bounds[2] = math.Max(into.Bounds[2], road.Bounds[2])
	into.Bounds[3] = math.Max(into.Bounds[3], road.Bounds[3])
}

func buildRoad(parsed *item) *Road {
	var raw [][]float64
	if err := json.Unmarshal(parsed.Nodes, &raw); err != nil {
		return nil
	}
	road := &Road{
		Class:       parsed.Class,
		Material:    parsed.Material,
		Group:       parsed.Parent,
		Drivability: parsed.Drivability,
		// The AI network is laid out as roads with an invisible material; they
		// are drivable but they are not the road surface a driver sees.
		Invisible: strings.Contains(strings.ToLower(parsed.Material), "invisible"),
		Bounds:    [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)},
	}
	for _, node := range raw {
		// A MeshRoad node carries a depth after its width; the first four
		// numbers mean the same thing in both classes.
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

// Filter says which roads to keep.
//
// DecalRoad is not a road class: BeamNG uses it for every ground decal there
// is — pavements, parking bays, kerbs, cracks, the concrete skirt around a
// building. Keeping everything draws a floor plan of the town, not a track.
//
// Drivability is the criterion the game itself uses. BeamNG's own map-making
// guide has level authors duplicate a road, set its material to road_invisible
// and its drivability to 1 to make it appear on the in-game minimap: the
// network the minimap draws IS the drivable, deliberately invisible AI layer.
// So an invisible material is a sign of a real road here, not a reason to drop
// one, and the painted decals are the things to leave out.
type Filter struct {
	// MinDrivability keeps only surfaces the game considers drivable. Decals
	// that are only paint or dirt carry no drivability at all.
	MinDrivability float64
	// VisibleOnly drops the invisible AI layer and keeps the rendered decals,
	// which is rarely what a map wants — see above.
	VisibleOnly bool
}

// DefaultFilter is what the map asks for: whatever the game will drive on.
func DefaultFilter() Filter {
	return Filter{MinDrivability: 0.01}
}

// Materials counts what a level is actually made of, so a filter can be chosen
// from evidence instead of guesswork.
type MaterialStat struct {
	Class          string  `json:"class"`
	Material       string  `json:"material"`
	Group          string  `json:"group"`
	Roads          int     `json:"roads"`
	Nodes          int     `json:"nodes"`
	Drivable       int     `json:"drivable"`
	MaxDrivability float64 `json:"maxDrivability"`
	MedianWidth    float64 `json:"medianWidth"`
}

func (level *Level) Materials() []MaterialStat {
	type bucket struct {
		stat   MaterialStat
		widths []float64
	}
	buckets := map[string]*bucket{}
	for _, road := range level.Roads {
		key := road.Class + "\x00" + road.Material + "\x00" + road.Group
		entry := buckets[key]
		if entry == nil {
			entry = &bucket{stat: MaterialStat{
				Class: road.Class, Material: road.Material, Group: road.Group}}
			buckets[key] = entry
		}
		entry.stat.Roads++
		entry.stat.Nodes += len(road.Nodes)
		if road.Drivability > 0 {
			entry.stat.Drivable++
		}
		if road.Drivability > entry.stat.MaxDrivability {
			entry.stat.MaxDrivability = road.Drivability
		}
		for _, node := range road.Nodes {
			entry.widths = append(entry.widths, node[3])
		}
	}
	out := make([]MaterialStat, 0, len(buckets))
	for _, entry := range buckets {
		sort.Float64s(entry.widths)
		if len(entry.widths) > 0 {
			entry.stat.MedianWidth = entry.widths[len(entry.widths)/2]
		}
		out = append(out, entry.stat)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Nodes > out[b].Nodes })
	return out
}

// Clip returns the roads overlapping a box, which is how a lap asks for just
// the roads it was driven on instead of a whole county.
func (level *Level) Clip(minX, minY, maxX, maxY float64, filter Filter) []*Road {
	out := make([]*Road, 0, 64)
	for _, road := range level.Roads {
		if filter.VisibleOnly && road.Invisible {
			continue
		}
		// A MeshRoad is structure, not paint: bridges and elevated sections are
		// built from it and often carry no drivability of their own, yet the
		// road plainly runs over them. Dropping them breaks the surface exactly
		// where a bridge is.
		if road.Class != "MeshRoad" && road.Drivability < filter.MinDrivability {
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
