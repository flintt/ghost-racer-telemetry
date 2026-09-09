package ghost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ImportPayload is what the mod posts to /api/import when the player exports
// selected records for analysis. Everything except Laps is optional: enough
// identity is reconstructed from whatever the payload carries.
type ImportPayload struct {
	Source    string      `json:"source"`
	Level     string      `json:"level"`
	StartID   string      `json:"startId"`
	StartName string      `json:"startName"`
	StartKey  string      `json:"startKey"`
	Kind      string      `json:"kind"`
	RaceKey   string      `json:"raceKey"`
	Library   string      `json:"library"`
	StartLine *StartLine  `json:"startLine"`
	Laps      []ImportLap `json:"laps"`

	// A single-lap upload may also be posted as a bare replay envelope.
	Samples RawSamples `json:"samples"`
	LapTime *float64   `json:"lapTime"`
	Vehicle string     `json:"vehicle"`
}

// ImportLap is one exported lap: its manifest fields plus the raw samples.
type ImportLap struct {
	ID               ID         `json:"id"`
	Label            string     `json:"label"`
	LapTime          *float64   `json:"lapTime"`
	Duration         *float64   `json:"duration"`
	SampleInterval   *float64   `json:"sampleInterval"`
	GroundOffset     *float64   `json:"groundOffset"`
	Source           string     `json:"source"`
	Complete         *bool      `json:"complete"`
	IncompleteReason string     `json:"incompleteReason"`
	Vehicle          string     `json:"vehicle"`
	Manual           bool       `json:"manual"`
	Color            string     `json:"color"`
	Pinned           bool       `json:"pinned"`
	HasInputs        bool       `json:"hasInputs"`
	ShareFingerprint string     `json:"shareFingerprint"`
	StartLine        *StartLine `json:"startLine"`
	Samples          RawSamples `json:"samples"`
}

// ImportResult reports what an import wrote.
type ImportResult struct {
	LibraryKey string   `json:"libraryKey"`
	Rel        string   `json:"rel"`
	Written    []string `json:"written"`
	Skipped    []string `json:"skipped"`
}

// Import writes an exported payload into the writable root, merging it into
// the manifest of the matching library so repeated exports of the same start
// accumulate instead of overwriting each other.
func Import(root Root, payload *ImportPayload) (*ImportResult, error) {
	if !root.Writable {
		return nil, errors.New("import root is not writable")
	}

	laps := payload.Laps
	if len(laps) == 0 && len(payload.Samples) > 0 {
		laps = []ImportLap{{
			ID:      ID(fmt.Sprintf("g%06d", time.Now().Unix()%1000000)),
			LapTime: payload.LapTime,
			Vehicle: payload.Vehicle,
			Samples: payload.Samples,
		}}
	}
	if len(laps) == 0 {
		return nil, errors.New("payload carries no laps")
	}

	rel, err := importLibraryRel(payload)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root.Path, filepath.FromSlash(rel))
	if !withinRoot(root.Path, base) {
		return nil, errors.New("library path escapes the import root")
	}
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		return nil, err
	}
	sampleDir := filepath.Join(root.Path, filepath.FromSlash(strings.TrimSuffix(rel, ".json")+".ghosts"))
	if err := os.MkdirAll(sampleDir, 0o755); err != nil {
		return nil, err
	}

	manifestFull := filepath.Join(root.Path, filepath.FromSlash(manifestPath(rel)))
	manifest, err := readManifest(manifestFull)
	if err != nil || manifest == nil {
		manifest = &Manifest{FormatVersion: 1, NextID: 1}
	}
	if payload.StartLine != nil {
		manifest.StartLine = payload.StartLine
	}

	result := &ImportResult{Rel: rel}
	for i := range laps {
		lap := &laps[i]
		id := strings.TrimSpace(lap.ID.String())
		if id == "" {
			// Match the mod's own id shape so an import is indistinguishable
			// from a lap the game recorded.
			id = fmt.Sprintf("g%06d", manifest.NextID)
		}
		id = sanitizePathPart(id, "lap")
		if len(lap.Samples) < 2 {
			result.Skipped = append(result.Skipped, id)
			continue
		}

		envelope := map[string]any{
			"formatVersion":    2,
			"sampleInterval":   floatOr(lap.SampleInterval, 0.02),
			"duration":         floatOr(lap.Duration, 0),
			"lapTime":          lap.LapTime,
			"vehicle":          lap.Vehicle,
			"groundOffset":     floatOr(lap.GroundOffset, 0),
			"complete":         boolOr(lap.Complete, true),
			"incompleteReason": lap.IncompleteReason,
			"shareFingerprint": lap.ShareFingerprint,
			"source":           lap.Source,
			"samples":          lap.Samples,
		}
		if lap.StartLine != nil {
			envelope["startLine"] = lap.StartLine
		} else if payload.StartLine != nil {
			envelope["startLine"] = payload.StartLine
		}
		samplePathFull := filepath.Join(sampleDir, id+".json")
		if err := writeJSONFile(samplePathFull, envelope); err != nil {
			return nil, err
		}

		descriptor := Descriptor{
			ID:               ID(id),
			Label:            lap.Label,
			LapTime:          lap.LapTime,
			Duration:         lap.Duration,
			SampleInterval:   lap.SampleInterval,
			GroundOffset:     lap.GroundOffset,
			Source:           lap.Source,
			Complete:         lap.Complete,
			IncompleteReason: lap.IncompleteReason,
			Vehicle:          lap.Vehicle,
			ImportedFrom:     payload.Source,
			Manual:           lap.Manual,
			Color:            lap.Color,
			Pinned:           lap.Pinned,
			HasInputs:        lap.HasInputs,
			File:             "ghostReplays/" + samplePath(rel, id),
		}
		replaceDescriptor(manifest, descriptor)
		// nextId counts laps, and ids are "g%06d" over that counter.
		if numeric, convErr := strconv.Atoi(strings.TrimPrefix(id, "g")); convErr == nil && numeric >= manifest.NextID {
			manifest.NextID = numeric + 1
		}
		result.Written = append(result.Written, id)
	}

	if err := writeJSONFile(manifestFull, manifest); err != nil {
		return nil, err
	}
	result.LibraryKey = root.Name + ":" + rel
	return result, nil
}

// importLibraryRel decides where an import lands, preferring the library path
// the mod reports so an export mirrors the in-game layout exactly.
func importLibraryRel(payload *ImportPayload) (string, error) {
	if payload.Library != "" {
		rel := strings.TrimPrefix(filepath.ToSlash(payload.Library), "ghostReplays/")
		rel = strings.TrimPrefix(rel, "/")
		if rel != "" && !strings.Contains(rel, "..") {
			if !strings.HasSuffix(rel, ".json") {
				rel += ".json"
			}
			return rel, nil
		}
	}

	level := sanitizePathPart(payload.Level, "unknown_level")
	if payload.RaceKey != "" || payload.Kind == "timeTrial" {
		return "races/" + level + "/" + sanitizePathPart(payload.RaceKey, "temp") + "/ghostracer.save.json", nil
	}
	if payload.StartID != "" {
		return "freeRoam/" + level + "/starts/" + sanitizePathPart(payload.StartID, "start") + "/ghostracer.save.json", nil
	}
	if payload.Level == "" {
		return "", fmt.Errorf("payload has neither a library path nor a level")
	}
	return "freeRoam/" + level + "/starts/imported/ghostracer.save.json", nil
}

func replaceDescriptor(manifest *Manifest, descriptor Descriptor) {
	for i := range manifest.Ghosts {
		if manifest.Ghosts[i].ID.String() == descriptor.ID.String() {
			manifest.Ghosts[i] = descriptor
			return
		}
	}
	manifest.Ghosts = append(manifest.Ghosts, descriptor)
}

// DeleteLap removes one lap's samples and its manifest entry.
func DeleteLap(rootPath string, library *Library, id string) error {
	manifestFull := filepath.Join(rootPath, filepath.FromSlash(manifestPath(library.Rel)))
	manifest, err := readManifest(manifestFull)
	if err != nil {
		return err
	}
	kept := manifest.Ghosts[:0]
	var removed *Descriptor
	for i := range manifest.Ghosts {
		if manifest.Ghosts[i].ID.String() == id {
			descriptor := manifest.Ghosts[i]
			removed = &descriptor
			continue
		}
		kept = append(kept, manifest.Ghosts[i])
	}
	if removed == nil {
		return fmt.Errorf("%w: lap %s", ErrNotFound, id)
	}
	manifest.Ghosts = kept
	if err := writeJSONFile(manifestFull, manifest); err != nil {
		return err
	}

	meta := descriptorToMeta(removed)
	if full, resolveErr := resolveSamplePath(rootPath, library, &meta); resolveErr == nil {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// writeJSONFile writes atomically so a half-written manifest can never replace
// a good one if the process dies mid-write.
func writeJSONFile(fullPath string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temp := fullPath + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, fullPath)
}
