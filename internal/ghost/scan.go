package ghost

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Root is one directory tree holding a ghostReplays layout. The game folder is
// mounted read-only; the import folder the service owns is writable.
type Root struct {
	Name     string
	Path     string
	Writable bool
}

// Library is one ghost library: a saved start, a track variant of one, or a
// race/Time Trial route. It owns a manifest and a directory of lap samples.
type Library struct {
	Key             string    `json:"key"`
	Root            string    `json:"root"`
	Rel             string    `json:"rel"`
	Kind            string    `json:"kind"`
	Level           string    `json:"level"`
	StartID         string    `json:"startId"`
	StartName       string    `json:"startName"`
	StartKey        string    `json:"startKey"`
	RaceKey         string    `json:"raceKey"`
	Vehicle         string    `json:"vehicle,omitempty"`
	Position        *Vec3     `json:"position,omitempty"`
	FinishPosition  *Vec3     `json:"finishPosition,omitempty"`
	PointToPoint    bool      `json:"pointToPoint"`
	Writable        bool      `json:"writable"`
	LapCount        int       `json:"lapCount"`
	CompleteCount   int       `json:"completeCount"`
	IncompleteCount int       `json:"incompleteCount"`
	ManualCount     int       `json:"manualCount"`
	BestLapTime     *float64  `json:"bestLapTime"`
	Vehicles        []string  `json:"vehicles"`
	ModifiedAt      time.Time `json:"modifiedAt"`
	Registered      bool      `json:"registered"`
}

// Catalog is the scanned view of every configured root.
type Catalog struct {
	ScannedAt time.Time  `json:"scannedAt"`
	Roots     []RootInfo `json:"roots"`
	Libraries []*Library `json:"libraries"`
}

// RootInfo reports one configured root and whether it actually exists.
type RootInfo struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Writable bool   `json:"writable"`
	Exists   bool   `json:"exists"`
	Count    int    `json:"count"`
}

// Scanner keeps the most recent catalog and re-reads on demand.
type Scanner struct {
	roots []Root

	mu      sync.RWMutex
	catalog *Catalog
	byKey   map[string]*Library
}

// NewScanner builds a scanner over the given roots, in priority order.
func NewScanner(roots []Root) *Scanner {
	return &Scanner{roots: roots, byKey: map[string]*Library{}}
}

// Roots returns the configured roots.
func (s *Scanner) Roots() []Root { return s.roots }

var unsafePathPart = regexp.MustCompile(`[^\w.\-]`)
var repeatedUnderscore = regexp.MustCompile(`_+`)

// sanitizePathPart mirrors the mod's Lua path sanitizer so registry ids and
// level names can be matched against the directories actually on disk.
func sanitizePathPart(value, fallback string) string {
	if value == "" {
		value = fallback
	}
	out := repeatedUnderscore.ReplaceAllString(unsafePathPart.ReplaceAllString(value, "_"), "_")
	if out == "" {
		return fallback
	}
	return out
}

// Scan re-reads every root and replaces the cached catalog.
func (s *Scanner) Scan() *Catalog {
	catalog := &Catalog{ScannedAt: time.Now()}
	byKey := map[string]*Library{}

	for _, root := range s.roots {
		info := RootInfo{Name: root.Name, Path: root.Path, Writable: root.Writable}
		if stat, err := os.Stat(root.Path); err == nil && stat.IsDir() {
			info.Exists = true
			libraries := scanRoot(root)
			info.Count = len(libraries)
			for _, library := range libraries {
				byKey[library.Key] = library
				catalog.Libraries = append(catalog.Libraries, library)
			}
		}
		catalog.Roots = append(catalog.Roots, info)
	}

	sort.Slice(catalog.Libraries, func(a, b int) bool {
		left, right := catalog.Libraries[a], catalog.Libraries[b]
		if left.Level != right.Level {
			return left.Level < right.Level
		}
		if left.StartKey != right.StartKey {
			return left.StartKey < right.StartKey
		}
		return left.Key < right.Key
	})

	s.mu.Lock()
	s.catalog = catalog
	s.byKey = byKey
	s.mu.Unlock()
	return catalog
}

// Catalog returns the cached catalog, scanning once if that has not happened.
func (s *Scanner) Catalog() *Catalog {
	s.mu.RLock()
	catalog := s.catalog
	s.mu.RUnlock()
	if catalog != nil {
		return catalog
	}
	return s.Scan()
}

// Lookup resolves a library key from the cached catalog.
func (s *Scanner) Lookup(key string) (*Library, bool) {
	s.mu.RLock()
	library, ok := s.byKey[key]
	s.mu.RUnlock()
	return library, ok
}

// RootPath returns the filesystem path of a named root.
func (s *Scanner) RootPath(name string) (Root, bool) {
	for _, root := range s.roots {
		if root.Name == name {
			return root, true
		}
	}
	return Root{}, false
}

// scanRoot finds every library below one root and enriches it from the
// per-level saved-start registries.
func scanRoot(root Root) []*Library {
	libraries := map[string]*Library{}

	_ = filepath.WalkDir(root.Path, func(fullPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory should not abort the whole scan; the game
			// may be writing into it right now.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			// Sample directories hold one file per lap and never a library.
			if strings.HasSuffix(entry.Name(), ".ghosts") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".library.json") {
			return nil
		}
		rel, relErr := filepath.Rel(root.Path, fullPath)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		base := strings.TrimSuffix(rel, ".library.json") + ".json"
		library := &Library{
			Key:      root.Name + ":" + base,
			Root:     root.Name,
			Rel:      base,
			Writable: root.Writable,
		}
		classifyLibraryPath(library)
		if stat, statErr := entry.Info(); statErr == nil {
			library.ModifiedAt = stat.ModTime()
		}
		summarizeLibrary(root, library)
		libraries[base] = library
		return nil
	})

	applyRegistries(root, libraries)

	out := make([]*Library, 0, len(libraries))
	for _, library := range libraries {
		out = append(out, library)
	}
	return out
}

// classifyLibraryPath derives level, start and race identity from the layout
// the mod writes. Unknown shapes stay usable, just without registry metadata.
func classifyLibraryPath(library *Library) {
	parts := strings.Split(library.Rel, "/")
	library.Kind = "other"
	library.Level = "unknown"

	switch {
	case len(parts) >= 5 && parts[0] == "freeRoam" && parts[2] == "starts":
		// freeRoam/<level>/starts/<startId>/ghostracer.save.json
		library.Kind = "freeRoam"
		library.Level = parts[1]
		library.StartID = parts[3]
	case len(parts) >= 6 && parts[0] == "freeRoam" && parts[3] == "starts":
		// pre-2.9.8: freeRoam/<level>/<vehicle>/starts/<startId>/...
		library.Kind = "legacy"
		library.Level = parts[1]
		library.Vehicle = parts[2]
		library.StartID = parts[4]
	case len(parts) >= 4 && parts[0] == "freeRoam":
		// pre-2.9.8 default library: freeRoam/<level>/<vehicle>/...
		library.Kind = "legacy"
		library.Level = parts[1]
		library.Vehicle = parts[2]
	case len(parts) >= 4 && parts[0] == "races":
		// races/<level>/<raceKey>/ghostracer.save.json
		library.Kind = "race"
		library.Level = parts[1]
		library.RaceKey = parts[2]
		library.StartID = parts[2]
	}

	if library.StartName == "" {
		switch {
		case library.RaceKey != "":
			library.StartName = library.RaceKey
		case library.StartID != "":
			library.StartName = library.StartID
		default:
			library.StartName = path.Dir(library.Rel)
		}
	}
	if library.StartKey == "" {
		library.StartKey = library.StartID
	}
}

// summarizeLibrary reads the manifest for the counts and best lap shown in the
// library browser, without touching the (much larger) sample files.
func summarizeLibrary(root Root, library *Library) {
	manifest, err := readManifest(filepath.Join(root.Path, filepath.FromSlash(manifestPath(library.Rel))))
	if err != nil {
		return
	}
	if manifest.StartLine != nil {
		position := manifest.StartLine.Position
		library.Position = &position
	}
	vehicles := map[string]bool{}
	for i := range manifest.Ghosts {
		descriptor := &manifest.Ghosts[i]
		if descriptor.File == "" {
			continue
		}
		library.LapCount++
		// Category order matches ghostCategory() in the mod: incomplete wins over
		// manual, manual over a timed lap.
		switch {
		case !boolOr(descriptor.Complete, true):
			library.IncompleteCount++
		case descriptor.Manual || descriptor.Source == "manual":
			library.ManualCount++
		default:
			library.CompleteCount++
			if descriptor.LapTime != nil && *descriptor.LapTime > 0 {
				if library.BestLapTime == nil || *descriptor.LapTime < *library.BestLapTime {
					lapTime := *descriptor.LapTime
					library.BestLapTime = &lapTime
				}
			}
		}
		if descriptor.Vehicle != "" {
			vehicles[descriptor.Vehicle] = true
		}
	}
	for vehicle := range vehicles {
		library.Vehicles = append(library.Vehicles, vehicle)
	}
	sort.Strings(library.Vehicles)
}

// applyRegistries overlays startLines.json so libraries carry the names, gate
// positions and track-variant grouping the player sees in game.
func applyRegistries(root Root, libraries map[string]*Library) {
	matches, _ := filepath.Glob(filepath.Join(root.Path, "freeRoam", "*", "startLines.json"))
	for _, registryPath := range matches {
		var registry Registry
		if err := readJSONFile(registryPath, &registry); err != nil {
			continue
		}
		level := sanitizePathPart(registry.Level, "unknown_level")
		if level == "unknown_level" {
			level = filepath.Base(filepath.Dir(registryPath))
		}
		for i := range registry.Lines {
			line := &registry.Lines[i]
			rel := libraryRelForLine(level, line)
			library, ok := libraries[rel]
			if !ok {
				continue
			}
			library.Registered = true
			library.StartName = line.Name
			library.StartKey = line.StartKey
			if library.StartKey == "" {
				library.StartKey = line.ID
			}
			library.StartID = line.ID
			if line.Kind == "timeTrial" {
				library.Kind = "timeTrial"
			}
			position := line.Position
			library.Position = &position
			if line.FinishPosition != nil {
				finish := *line.FinishPosition
				library.FinishPosition = &finish
				library.PointToPoint = true
			}
		}
	}
}

// libraryRelForLine mirrors registry.lineLibraryFilename in the mod.
func libraryRelForLine(level string, line *RegistryLine) string {
	if line.Kind == "timeTrial" && line.RaceKey != "" {
		return "races/" + sanitizePathPart(level, "unknown_level") + "/" +
			sanitizePathPart(line.RaceKey, "temp") + "/ghostracer.save.json"
	}
	return "freeRoam/" + sanitizePathPart(level, "unknown_level") + "/starts/" +
		sanitizePathPart(line.ID, "start") + "/ghostracer.save.json"
}

// manifestPath maps a library base file to its manifest, as the mod does.
func manifestPath(rel string) string {
	if strings.HasSuffix(rel, ".json") {
		return strings.TrimSuffix(rel, ".json") + ".library.json"
	}
	return rel + ".library.json"
}

// samplePath maps a library base file and lap id to that lap's sample file.
func samplePath(rel, id string) string {
	stem := strings.TrimSuffix(rel, ".json")
	return stem + ".ghosts/" + id + ".json"
}

func readJSONFile(fullPath string, target any) error {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func readManifest(fullPath string) (*Manifest, error) {
	var manifest Manifest
	if err := readJSONFile(fullPath, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}
