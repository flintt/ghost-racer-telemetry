package main

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdirAll(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(parts...)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveGameRootAcceptsUserFolderOrReplaysDir(t *testing.T) {
	home := t.TempDir()
	userFolder := filepath.Join(home, "BeamNG", "BeamNG.drive", "current")
	replays := mkdirAll(t, userFolder, "ghostReplays")

	cases := map[string]string{
		"the ghostReplays directory itself": replays,
		"the versioned user folder":         userFolder,
		"the user folder above the version": filepath.Join(home, "BeamNG", "BeamNG.drive"),
		// A Windows shell turns a quoted path ending in \ into one with a
		// trailing quote character; that must not defeat the lookup.
		"a path with a stray trailing quote": replays + `"`,
	}
	for name, configured := range cases {
		resolved, tried, err := resolveGameRoot(configured)
		if err != nil {
			t.Errorf("%s: %v (tried %v)", name, err, tried)
			continue
		}
		if resolved != replays {
			t.Errorf("%s: resolved %s, want %s", name, resolved, replays)
		}
	}
}

func TestResolveGameRootAutodetectsVendorLayout(t *testing.T) {
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	// Current installs nest the game under a vendor directory and keep both a
	// "current" junction and the numbered folders.
	base := filepath.Join(local, "BeamNG", "BeamNG.drive")
	mkdirAll(t, base, "0.35", "ghostReplays")
	current := mkdirAll(t, base, "current", "ghostReplays")

	resolved, tried, err := resolveGameRoot("")
	if err != nil {
		t.Fatalf("%v (tried %v)", err, tried)
	}
	if resolved != current {
		t.Errorf("resolved %s, want the current junction %s", resolved, current)
	}
}

func TestResolveGameRootReportsWhatItTried(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "empty"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "empty"))
	_, tried, err := resolveGameRoot("")
	if err == nil {
		t.Fatal("expected a failure when nothing is installed")
	}
	if len(tried) == 0 {
		t.Error("the failure must list the paths that were checked")
	}
}

func TestLooksLikeGameFolder(t *testing.T) {
	folder := t.TempDir()
	if looksLikeGameFolder(folder) {
		t.Error("an empty import directory is not a game folder")
	}
	registry := mkdirAll(t, folder, "freeRoam", "east_coast_usa")
	if err := os.WriteFile(filepath.Join(registry, "startLines.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !looksLikeGameFolder(folder) {
		t.Error("a tree with startLines.json is the game's own folder")
	}
}
