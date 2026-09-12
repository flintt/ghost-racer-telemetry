// Command bng_ghost_web serves a local telemetry browser for Ghost Racer
// Enhanced recordings: it parses the ghostReplays tree BeamNG writes and, on
// the same loopback port, accepts laps exported from the in-game app.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flintt/ghost-racer-telemetry/internal/ghost"
	"github.com/flintt/ghost-racer-telemetry/internal/roads"
)

//go:embed web
var embeddedWeb embed.FS

// version is stamped at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

type server struct {
	scanner     *ghost.Scanner
	importRoot  ghost.Root
	allowDelete bool
	token       string
	gameInstall string
	roads       *roads.Store
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8777", "listen address; keep it on loopback unless you mean to expose it")
	gameRoot := flag.String("root", "", "BeamNG user folder or its ghostReplays directory (auto-detected when empty)")
	dataDir := flag.String("data", defaultDataDir(), "writable directory for laps exported from the game")
	webDir := flag.String("web", "", "serve the UI from this directory instead of the embedded copy")
	allowDelete := flag.Bool("allow-delete", false, "allow deleting laps from the game folder (close BeamNG first)")
	token := flag.String("token", "", "require this token on /api/import as ?token= or X-Ghost-Token")
	gameInstall := flag.String("game", "", "BeamNG install directory, for reading road geometry out of the level archives (auto-detected when empty)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	resolved, tried, err := resolveGameRoot(*gameRoot)
	if err != nil {
		log.Printf("game folder: %v", err)
		for _, candidate := range tried {
			log.Printf("  tried %s", candidate)
		}
		log.Printf(`  pass -root "<BeamNG user folder>", e.g. %%LOCALAPPDATA%%\BeamNG\BeamNG.drive\current`)
	}

	importRoot := ghost.Root{Name: "import", Path: *dataDir, Writable: true}
	if err := os.MkdirAll(importRoot.Path, 0o755); err != nil {
		log.Fatalf("cannot create import directory %s: %v", importRoot.Path, err)
	}

	roots := []ghost.Root{}
	if resolved != "" {
		roots = append(roots, ghost.Root{Name: "game", Path: resolved})
	}
	roots = append(roots, importRoot)

	install := resolveGameInstall(*gameInstall)
	app := &server{
		scanner:     ghost.NewScanner(roots),
		importRoot:  importRoot,
		allowDelete: *allowDelete,
		token:       *token,
		gameInstall: install,
		roads:       roads.NewStore(filepath.Join(filepath.Dir(importRoot.Path), "roads")),
	}
	catalog := app.scanner.Scan()

	mux := http.NewServeMux()
	mux.Handle("/", uiHandler(*webDir))
	mux.HandleFunc("/api/catalog", app.handleCatalog)
	mux.HandleFunc("/api/rescan", app.handleRescan)
	mux.HandleFunc("/api/laps", app.handleLaps)
	mux.HandleFunc("/api/lap", app.handleLap)
	mux.HandleFunc("/api/import", app.handleImport)
	mux.HandleFunc("/api/roads", app.handleRoads)

	fmt.Printf("Ghost Racer telemetry web %s\n", version)
	for _, root := range catalog.Roots {
		state := "missing"
		if root.Exists {
			state = fmt.Sprintf("%d libraries", root.Count)
		}
		fmt.Printf("  %-6s %s  (%s)\n", root.Name, root.Path, state)
	}
	if looksLikeGameFolder(importRoot.Path) {
		// -data is writable, so a lap deleted there is really gone from the
		// game. That is almost never what someone means by pointing it at the
		// save folder; -root is the read-only flag they wanted.
		fmt.Printf("\n  !! -data points at what looks like BeamNG's own save folder.\n")
		fmt.Printf("     Laps there can be deleted for real, with no -allow-delete guard.\n")
		fmt.Printf("     To browse the game's recordings read-only, use -root instead:\n")
		fmt.Printf("       %s -root %q\n\n", filepath.Base(os.Args[0]), importRoot.Path)
	}
	if install != "" {
		fmt.Printf("  %-6s %s  (road geometry)\n", "levels", install)
	}
	fmt.Printf("  open   http://%s/\n", *addr)
	if err := http.ListenAndServe(*addr, logRequests(mux)); err != nil {
		log.Fatal(err)
	}
}

// resolveGameRoot accepts the BeamNG user folder, one of its version folders,
// or the ghostReplays directory itself. It returns every path it tried so a
// failure can say exactly where it looked.
func resolveGameRoot(configured string) (string, []string, error) {
	var candidates []string
	if configured != "" {
		// A Windows shell folds a trailing backslash into the closing quote, so
		// "…\ghostReplays\" arrives with a stray quote character attached.
		cleaned := strings.TrimSpace(strings.Trim(configured, `"'`))
		cleaned = strings.TrimRight(cleaned, `\/`)
		candidates = append(candidates, cleaned, filepath.Join(cleaned, "ghostReplays"))
		candidates = append(candidates, versionedCandidates(cleaned)...)
	} else {
		for _, base := range defaultUserFolders() {
			candidates = append(candidates, filepath.Join(base, "ghostReplays"))
			candidates = append(candidates, versionedCandidates(base)...)
		}
	}

	// Prefer a candidate that really is a replay tree: -root is often given as
	// the user folder, which exists but holds settings and mods, not laps.
	for _, candidate := range candidates {
		if isReplaysDir(candidate) {
			absolute, _ := filepath.Abs(candidate)
			return absolute, candidates, nil
		}
	}
	// An unrecognised but existing directory is still worth scanning: someone
	// may keep recordings in a copy that has no freeRoam/races yet.
	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() &&
			strings.EqualFold(filepath.Base(candidate), "ghostReplays") {
			absolute, _ := filepath.Abs(candidate)
			return absolute, candidates, nil
		}
	}
	if configured != "" {
		return "", candidates, fmt.Errorf("no ghostReplays directory at or below %s", configured)
	}
	return "", candidates, errors.New("no BeamNG ghostReplays directory found; pass -root")
}

// isReplaysDir reports whether a directory is a ghostReplays tree, either by
// name or by holding the subdirectories the mod writes into.
func isReplaysDir(path string) bool {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return false
	}
	if strings.EqualFold(filepath.Base(path), "ghostReplays") {
		return true
	}
	for _, name := range []string{"freeRoam", "races"} {
		if child, err := os.Stat(filepath.Join(path, name)); err == nil && child.IsDir() {
			return true
		}
	}
	return false
}

// versionedCandidates expands a user folder into its version directories. The
// install keeps a "current" junction next to the numbered folders (0.36, 0.35…),
// so that one is tried first and the rest newest-first.
func versionedCandidates(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		// A Windows junction reports as a symlink rather than a directory.
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	candidates := make([]string, 0, len(names))
	for _, name := range names {
		if strings.EqualFold(name, "current") {
			candidates = append(candidates, filepath.Join(base, name, "ghostReplays"))
		}
	}
	for _, name := range names {
		if !strings.EqualFold(name, "current") {
			candidates = append(candidates, filepath.Join(base, name, "ghostReplays"))
		}
	}
	return candidates
}

func defaultUserFolders() []string {
	var folders []string
	home, _ := os.UserHomeDir()
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		// Current installs nest the game under a vendor directory; older ones
		// put BeamNG.drive straight into LocalAppData.
		folders = append(folders,
			filepath.Join(local, "BeamNG", "BeamNG.drive"),
			filepath.Join(local, "BeamNG.drive"),
		)
	}
	if home != "" {
		folders = append(folders,
			filepath.Join(home, "AppData", "Local", "BeamNG", "BeamNG.drive"),
			filepath.Join(home, "AppData", "Local", "BeamNG.drive"),
			filepath.Join(home, "Documents", "BeamNG.drive"),
			filepath.Join(home, ".local", "share", "BeamNG.drive"),
		)
	}
	return folders
}

// looksLikeGameFolder reports whether a directory holds the saved-start
// registries only BeamNG itself writes, so pointing -data at the game folder
// can be called out instead of silently making it deletable.
func looksLikeGameFolder(path string) bool {
	matches, _ := filepath.Glob(filepath.Join(path, "freeRoam", "*", "startLines.json"))
	return len(matches) > 0
}

// resolveGameInstall finds the BeamNG installation — not the user folder, the
// place the level archives live. Steam keeps its libraries in a text manifest,
// which is the only reliable way to find an install on a second drive.
func resolveGameInstall(configured string) string {
	candidates := []string{}
	if configured != "" {
		candidates = append(candidates, strings.TrimRight(strings.Trim(strings.TrimSpace(configured), `"'`), `\/`))
	} else {
		for _, library := range steamLibraries() {
			candidates = append(candidates, filepath.Join(library, "steamapps", "common", "BeamNG.drive"))
		}
	}
	for _, candidate := range candidates {
		if stat, err := os.Stat(filepath.Join(candidate, "content", "levels")); err == nil && stat.IsDir() {
			absolute, _ := filepath.Abs(candidate)
			return absolute
		}
	}
	if configured != "" {
		log.Printf("game install: no content/levels under %s", configured)
	}
	return ""
}

var steamPathLine = regexp.MustCompile(`"path"\s+"([^"]+)"`)

func steamLibraries() []string {
	roots := []string{}
	home, _ := os.UserHomeDir()
	for _, variable := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if base := os.Getenv(variable); base != "" {
			roots = append(roots, filepath.Join(base, "Steam"))
		}
	}
	if home != "" {
		roots = append(roots,
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, "Library", "Application Support", "Steam"),
		)
	}

	libraries := []string{}
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		libraries = append(libraries, path)
	}
	for _, root := range roots {
		add(root)
		// Other drives are listed in the library manifest.
		data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, match := range steamPathLine.FindAllStringSubmatch(string(data), -1) {
			add(strings.ReplaceAll(match[1], `\\`, `\`))
		}
	}
	return libraries
}

func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return filepath.Join(".", "bng_ghost_web_data", "ghostReplays")
	}
	return filepath.Join(base, "bng_ghost_web", "ghostReplays")
}

func uiHandler(webDir string) http.Handler {
	if webDir != "" {
		return http.FileServer(http.Dir(webDir))
	}
	sub, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("embedded UI: %v", err)
	}
	return http.FileServer(http.FS(sub))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		start := time.Now()
		next.ServeHTTP(writer, request)
		if strings.HasPrefix(request.URL.Path, "/api/") {
			log.Printf("%s %s %s", request.Method, request.URL.RequestURI(), time.Since(start).Round(time.Millisecond))
		}
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

// library resolves the ?lib= key to a library and the root holding it.
func (s *server) library(request *http.Request) (*ghost.Library, ghost.Root, error) {
	key := request.URL.Query().Get("lib")
	if key == "" {
		return nil, ghost.Root{}, errors.New("missing lib parameter")
	}
	library, ok := s.scanner.Lookup(key)
	if !ok {
		// A library recorded after the last scan is only a rescan away.
		s.scanner.Scan()
		if library, ok = s.scanner.Lookup(key); !ok {
			return nil, ghost.Root{}, fmt.Errorf("unknown library %s", key)
		}
	}
	root, ok := s.scanner.RootPath(library.Root)
	if !ok {
		return nil, ghost.Root{}, fmt.Errorf("unknown root %s", library.Root)
	}
	return library, root, nil
}

// handleRoads serves the road geometry around a lap. The box comes from the
// caller because only the client knows which part of a county it is looking at.
func (s *server) handleRoads(writer http.ResponseWriter, request *http.Request) {
	level := request.URL.Query().Get("level")
	if level == "" {
		writeError(writer, http.StatusBadRequest, "missing level parameter")
		return
	}
	extracted, err := s.roads.Load(s.gameInstall, level)
	if err != nil {
		writeError(writer, http.StatusNotFound, err.Error())
		return
	}

	query := request.URL.Query()
	minX := floatParam(query.Get("minx"), extracted.Bounds[0])
	minY := floatParam(query.Get("miny"), extracted.Bounds[1])
	maxX := floatParam(query.Get("maxx"), extracted.Bounds[2])
	maxY := floatParam(query.Get("maxy"), extracted.Bounds[3])
	margin := floatParam(query.Get("margin"), 120)
	clipped := extracted.Clip(minX-margin, minY-margin, maxX+margin, maxY+margin,
		query.Get("ai") == "1")

	nodes := 0
	for _, road := range clipped {
		nodes += len(road.Nodes)
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"level":      extracted.Level,
		"source":     extracted.Source,
		"roads":      clipped,
		"roadCount":  len(clipped),
		"nodeCount":  nodes,
		"totalRoads": len(extracted.Roads),
	})
}

func floatParam(raw string, fallback float64) float64 {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}

func (s *server) handleCatalog(writer http.ResponseWriter, request *http.Request) {
	catalog := s.scanner.Catalog()
	writeJSON(writer, http.StatusOK, catalog)
}

func (s *server) handleRescan(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, s.scanner.Scan())
}

func (s *server) handleLaps(writer http.ResponseWriter, request *http.Request) {
	library, root, err := s.library(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	laps, err := ghost.ListLaps(root.Path, library)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ghost.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(writer, status, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"library": library, "laps": laps})
}

func (s *server) handleLap(writer http.ResponseWriter, request *http.Request) {
	library, root, err := s.library(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	id := request.URL.Query().Get("id")
	if id == "" {
		writeError(writer, http.StatusBadRequest, "missing id parameter")
		return
	}

	if request.Method == http.MethodDelete {
		if !library.Writable && !s.allowDelete {
			writeError(writer, http.StatusForbidden,
				"deleting from the game folder is disabled; restart with -allow-delete and close BeamNG first")
			return
		}
		if err := ghost.DeleteLap(root.Path, library, id); err != nil {
			writeError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		s.scanner.Scan()
		writeJSON(writer, http.StatusOK, map[string]any{"deleted": id})
		return
	}

	lap, err := ghost.LoadLap(root.Path, library, id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ghost.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(writer, status, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, lap)
}

// handleImport accepts laps exported from the in-game app. It is CORS-open on
// purpose: the BeamNG UI is a CEF page with its own origin, and the listener is
// on loopback. Set -token to require a shared secret.
func (s *server) handleImport(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ghost-Token")
	writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	if request.Method == http.MethodOptions {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "POST a payload")
		return
	}
	if s.token != "" {
		supplied := request.Header.Get("X-Ghost-Token")
		if supplied == "" {
			supplied = request.URL.Query().Get("token")
		}
		if supplied != s.token {
			writeError(writer, http.StatusUnauthorized, "bad token")
			return
		}
	}

	var payload ghost.ImportPayload
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 256<<20))
	if err := decoder.Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "parse payload: "+err.Error())
		return
	}
	result, err := ghost.Import(s.importRoot, &payload)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	s.scanner.Scan()
	log.Printf("imported %d lap(s) into %s", len(result.Written), result.Rel)
	writeJSON(writer, http.StatusOK, result)
}
