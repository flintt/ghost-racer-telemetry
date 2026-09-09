// Package ghost reads Ghost Racer Enhanced telemetry written by the BeamNG mod
// and turns it into channels a browser can plot.
//
// On-disk layout (relative to the BeamNG user folder):
//
//	ghostReplays/freeRoam/<level>/startLines.json                       saved-start registry
//	ghostReplays/freeRoam/<level>/starts/<id>/ghostracer.save.library.json   lap manifest
//	ghostReplays/freeRoam/<level>/starts/<id>/ghostracer.save.ghosts/<gid>.json  samples
//	ghostReplays/races/<level>/<raceKey>/ghostracer.save.*              Time Trial / race
//	ghostReplays/freeRoam/<level>/<vehicleDir>/starts/<id>/...          pre-2.9.8 layout
package ghost

import (
	"bytes"
	"encoding/json"
	"math"
)

// Sample column indexes, mirroring lua/vehicle/controller/ghostRacer.lua.
const (
	IdxTime = iota
	IdxPosX
	IdxPosY
	IdxPosZ
	IdxFrontX
	IdxFrontY
	IdxFrontZ
	IdxUpX
	IdxUpY
	IdxUpZ
	IdxSpeed
	IdxThrottle
	IdxBrake
	IdxGear
	IdxHandbrake
	IdxClutch
	sampleColumns
)

// Registry is startLines.json: every saved start on one level.
type Registry struct {
	FormatVersion int            `json:"formatVersion"`
	Revision      int            `json:"revision"`
	Level         string         `json:"level"`
	ActiveID      string         `json:"activeId"`
	Lines         []RegistryLine `json:"lines"`
}

// RegistryLine is one saved start. Lines sharing a StartKey are track variants
// of the same physical start position, each with its own ghost library.
type RegistryLine struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	UserNamed      bool   `json:"userNamed"`
	StartKey       string `json:"startKey"`
	Kind           string `json:"kind"`
	RaceKey        string `json:"raceKey"`
	ActivitySource string `json:"activitySource"`
	Position       Vec3   `json:"position"`
	Normal         Vec3   `json:"normal"`
	FinishPosition *Vec3  `json:"finishPosition"`
	FinishNormal   *Vec3  `json:"finishNormal"`
}

// Manifest is ghostracer.save.library.json: the index of stored laps.
type Manifest struct {
	FormatVersion int          `json:"formatVersion"`
	NextID        int          `json:"nextId"`
	DisplayMode   string       `json:"displayMode"`
	StartLine     *StartLine   `json:"startLine"`
	Ghosts        []Descriptor `json:"ghosts"`
}

// ID is a lap id. The mod writes it as a string ("g000001"); hand-made and
// very old files sometimes carry a bare number, so both decode.
type ID string

func (i *ID) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*i = ""
		return nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		*i = ID(text)
		return nil
	}
	*i = ID(trimmed)
	return nil
}

// MarshalJSON always writes the string form, which is what the mod reads back.
func (i ID) MarshalJSON() ([]byte, error) { return json.Marshal(string(i)) }

func (i ID) String() string { return string(i) }

// Descriptor is one lap's manifest entry.
type Descriptor struct {
	ID               ID       `json:"id"`
	Label            string   `json:"label"`
	LapTime          *float64 `json:"lapTime"`
	Duration         *float64 `json:"duration"`
	SampleInterval   *float64 `json:"sampleInterval"`
	GroundOffset     *float64 `json:"groundOffset"`
	HasSpeed         *bool    `json:"hasSpeed"`
	Source           string   `json:"source"`
	Complete         *bool    `json:"complete"`
	IncompleteReason string   `json:"incompleteReason"`
	Vehicle          string   `json:"vehicle"`
	ImportedFrom     string   `json:"importedFrom"`
	Manual           bool     `json:"manual"`
	Color            string   `json:"color"`
	Selected         bool     `json:"selected"`
	Pinned           bool     `json:"pinned"`
	HasInputs        bool     `json:"hasInputs"`
	File             string   `json:"file"`
}

// StartLine is the gate geometry stored alongside a library or an envelope.
type StartLine struct {
	Level     string  `json:"level"`
	Position  Vec3    `json:"position"`
	Normal    Vec3    `json:"normal"`
	HalfWidth float64 `json:"halfWidth"`
}

// Envelope is one lap's sample file (replay format 2, with format 1 tolerated).
type Envelope struct {
	FormatVersion    int        `json:"formatVersion"`
	SampleInterval   float64    `json:"sampleInterval"`
	Duration         float64    `json:"duration"`
	LapTime          *float64   `json:"lapTime"`
	Vehicle          string     `json:"vehicle"`
	GroundOffset     float64    `json:"groundOffset"`
	Complete         *bool      `json:"complete"`
	IncompleteReason string     `json:"incompleteReason"`
	ShareFingerprint string     `json:"shareFingerprint"`
	StartLine        *StartLine `json:"startLine"`
	Samples          RawSamples `json:"samples"`
}

// Vec3 tolerates both the array form the mod writes and an {x,y,z} object.
type Vec3 [3]float64

func (v *Vec3) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] == '{' {
		var obj struct {
			X, Y, Z float64
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		*v = Vec3{obj.X, obj.Y, obj.Z}
		return nil
	}
	var arr []float64
	if err := json.Unmarshal(data, &arr); err != nil {
		// An empty Lua table serializes as {} and is handled above; anything
		// else that is not an array is simply treated as absent.
		return nil
	}
	for i := 0; i < len(arr) && i < 3; i++ {
		v[i] = arr[i]
	}
	return nil
}

// RawSamples holds the sample rows exactly as stored. Lua serializes an empty
// table as {}, so a non-array value decodes to an empty slice rather than an
// error, and rows may be either the flat numeric array (format 2) or the
// {pos,dirFront,dirUp,speed} object of the original 1.6 format.
type RawSamples []json.RawMessage

func (s *RawSamples) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '[' {
		*s = nil
		return nil
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return err
	}
	*s = rows
	return nil
}

// UnmarshalGhosts is the manifest ghost list, tolerant of {} for "empty".
func (m *Manifest) UnmarshalJSON(data []byte) error {
	type manifestAlias Manifest
	var raw struct {
		manifestAlias
		Ghosts json.RawMessage `json:"ghosts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = Manifest(raw.manifestAlias)
	trimmed := bytes.TrimSpace(raw.Ghosts)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &m.Ghosts); err != nil {
			return err
		}
	}
	return nil
}

func boolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func floatOr(value *float64, fallback float64) float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return fallback
	}
	return *value
}
