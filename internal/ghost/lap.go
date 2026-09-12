package ghost

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotFound reports a lap or library that is not on disk.
var ErrNotFound = errors.New("not found")

// LapMeta is one lap as listed in a library, without its samples.
type LapMeta struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	LapTime          *float64 `json:"lapTime"`
	Duration         float64  `json:"duration"`
	SampleInterval   float64  `json:"sampleInterval"`
	Source           string   `json:"source"`
	Complete         bool     `json:"complete"`
	IncompleteReason string   `json:"incompleteReason,omitempty"`
	Manual           bool     `json:"manual"`
	Vehicle          string   `json:"vehicle"`
	ImportedFrom     string   `json:"importedFrom,omitempty"`
	Color            string   `json:"color"`
	Pinned           bool     `json:"pinned"`
	HasInputs        bool     `json:"hasInputs"`
	Category         string   `json:"category"`
	File             string   `json:"file"`
	Rank             int      `json:"rank"`
}

// Lap is one lap with its decoded channels and summary metrics.
type Lap struct {
	LapMeta
	LibraryKey string     `json:"libraryKey"`
	Level      string     `json:"level"`
	StartName  string     `json:"startName"`
	StartLine  *StartLine `json:"startLine,omitempty"`
	Summary    Summary    `json:"summary"`
	Channels   Channels   `json:"channels"`
}

// Summary is the per-lap readout shown above the charts.
type Summary struct {
	SampleCount     int        `json:"sampleCount"`
	Distance        float64    `json:"distance"`
	Duration        float64    `json:"duration"`
	TopSpeed        float64    `json:"topSpeed"`
	MinSpeed        float64    `json:"minSpeed"`
	AvgSpeed        float64    `json:"avgSpeed"`
	MaxAccel        float64    `json:"maxAccel"`
	MaxDecel        float64    `json:"maxDecel"`
	MaxLatG         float64    `json:"maxLatG"`
	FullThrottlePct float64    `json:"fullThrottlePct"`
	BrakingPct      float64    `json:"brakingPct"`
	CoastingPct     float64    `json:"coastingPct"`
	ElevationGain   float64    `json:"elevationGain"`
	MaxGradient     float64    `json:"maxGradient"`
	MinGradient     float64    `json:"minGradient"`
	MinZ            float64    `json:"minZ"`
	MaxZ            float64    `json:"maxZ"`
	Bounds          [4]float64 `json:"bounds"`
}

// Channels are the per-sample series, all the same length.
type Channels struct {
	T         []float64 `json:"t"`
	Dist      []float64 `json:"dist"`
	X         []float64 `json:"x"`
	Y         []float64 `json:"y"`
	Z         []float64 `json:"z"`
	Speed     []float64 `json:"speed"`
	Accel     []float64 `json:"accel"`
	Gradient  []float64 `json:"gradient"`
	LatG      []float64 `json:"latG"`
	Heading   []float64 `json:"heading"`
	Throttle  []float64 `json:"throttle,omitempty"`
	Brake     []float64 `json:"brake,omitempty"`
	Gear      []float64 `json:"gear,omitempty"`
	Handbrake []float64 `json:"handbrake,omitempty"`
	Clutch    []float64 `json:"clutch,omitempty"`
}

const gravity = 9.80665

// Gradient is read over this much travelled distance either side. Differencing
// neighbouring samples would measure the suspension, not the road: at 50 Hz two
// samples are centimetres apart and the height noise swamps the slope.
const gradientWindow = 10.0

// ListLaps reads a library manifest and returns its laps, ranked by lap time.
func ListLaps(rootPath string, library *Library) ([]LapMeta, error) {
	manifest, err := readManifest(filepath.Join(rootPath, filepath.FromSlash(manifestPath(library.Rel))))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: manifest for %s", ErrNotFound, library.Key)
		}
		return nil, err
	}

	laps := make([]LapMeta, 0, len(manifest.Ghosts))
	for i := range manifest.Ghosts {
		descriptor := &manifest.Ghosts[i]
		if descriptor.File == "" {
			continue
		}
		laps = append(laps, descriptorToMeta(descriptor))
	}
	rankLaps(laps)
	return laps, nil
}

func descriptorToMeta(descriptor *Descriptor) LapMeta {
	meta := LapMeta{
		ID:               descriptor.ID.String(),
		Label:            descriptor.Label,
		LapTime:          descriptor.LapTime,
		Duration:         floatOr(descriptor.Duration, 0),
		SampleInterval:   floatOr(descriptor.SampleInterval, 0),
		Source:           descriptor.Source,
		Complete:         boolOr(descriptor.Complete, true),
		IncompleteReason: descriptor.IncompleteReason,
		Manual:           descriptor.Manual || descriptor.Source == "manual",
		Vehicle:          descriptor.Vehicle,
		ImportedFrom:     descriptor.ImportedFrom,
		Color:            descriptor.Color,
		Pinned:           descriptor.Pinned,
		HasInputs:        descriptor.HasInputs,
		File:             descriptor.File,
	}
	switch {
	case !meta.Complete:
		meta.Category = "incomplete"
	case meta.Manual:
		meta.Category = "manual"
	default:
		meta.Category = "lap"
	}
	if meta.Label == "" {
		meta.Label = "Lap " + meta.ID
	}
	return meta
}

// rankLaps assigns 1-based ranks over timed, complete laps only; everything
// else keeps rank 0 so the UI can leave it unranked.
func rankLaps(laps []LapMeta) {
	timed := make([]int, 0, len(laps))
	for i := range laps {
		if laps[i].Category == "lap" && laps[i].LapTime != nil && *laps[i].LapTime > 0 {
			timed = append(timed, i)
		}
	}
	sort.SliceStable(timed, func(a, b int) bool {
		return *laps[timed[a]].LapTime < *laps[timed[b]].LapTime
	})
	for rank, index := range timed {
		laps[index].Rank = rank + 1
	}
}

// resolveSamplePath maps a lap to a file inside the root, refusing anything
// that would escape it. The manifest stores user-folder-relative paths, so the
// ghostReplays prefix is stripped when the root is that directory itself.
func resolveSamplePath(rootPath string, library *Library, meta *LapMeta) (string, error) {
	candidates := []string{samplePath(library.Rel, meta.ID)}
	if meta.File != "" {
		candidates = append(candidates, strings.TrimPrefix(filepath.ToSlash(meta.File), "ghostReplays/"))
	}
	for _, candidate := range candidates {
		full := filepath.Join(rootPath, filepath.FromSlash(candidate))
		if !withinRoot(rootPath, full) {
			continue
		}
		if _, err := os.Stat(full); err == nil {
			return full, nil
		}
	}
	return "", fmt.Errorf("%w: samples for lap %s", ErrNotFound, meta.ID)
}

func withinRoot(rootPath, full string) bool {
	rel, err := filepath.Rel(rootPath, full)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// LoadLap reads one lap's samples and derives its channels and summary.
func LoadLap(rootPath string, library *Library, id string) (*Lap, error) {
	laps, err := ListLaps(rootPath, library)
	if err != nil {
		return nil, err
	}
	var meta *LapMeta
	for i := range laps {
		if laps[i].ID == id {
			meta = &laps[i]
			break
		}
	}
	if meta == nil {
		return nil, fmt.Errorf("%w: lap %s in %s", ErrNotFound, id, library.Key)
	}

	fullPath, err := resolveSamplePath(rootPath, library, meta)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse %s: %w", fullPath, err)
	}

	lap := &Lap{
		LapMeta:    *meta,
		LibraryKey: library.Key,
		Level:      library.Level,
		StartName:  library.StartName,
		StartLine:  envelope.StartLine,
	}
	if lap.SampleInterval == 0 {
		lap.SampleInterval = envelope.SampleInterval
	}
	if lap.LapTime == nil && envelope.LapTime != nil {
		lap.LapTime = envelope.LapTime
	}
	if lap.Vehicle == "" {
		lap.Vehicle = envelope.Vehicle
	}
	buildChannels(lap, &envelope)
	return lap, nil
}

// decodeRow accepts both replay format 2 (a flat numeric array) and the
// original 1.6 rows ({pos, dirFront, dirUp, speed}).
func decodeRow(raw json.RawMessage, index int, interval float64) ([sampleColumns]float64, bool) {
	var row [sampleColumns]float64
	for i := range row {
		row[i] = math.NaN()
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return row, false
	}
	if trimmed[0] == '[' {
		var values []*float64
		if err := json.Unmarshal(raw, &values); err != nil {
			return row, false
		}
		if len(values) < IdxSpeed+1 {
			return row, false
		}
		for i := 0; i < len(values) && i < sampleColumns; i++ {
			if values[i] != nil {
				row[i] = *values[i]
			}
		}
		if math.IsNaN(row[IdxTime]) {
			row[IdxTime] = float64(index) * interval
		}
		return row, !math.IsNaN(row[IdxPosX]) && !math.IsNaN(row[IdxPosY])
	}

	var legacy struct {
		Pos      Vec3     `json:"pos"`
		DirFront Vec3     `json:"dirFront"`
		DirUp    Vec3     `json:"dirUp"`
		Speed    *float64 `json:"speed"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return row, false
	}
	row[IdxTime] = float64(index) * interval
	row[IdxPosX], row[IdxPosY], row[IdxPosZ] = legacy.Pos[0], legacy.Pos[1], legacy.Pos[2]
	row[IdxFrontX], row[IdxFrontY], row[IdxFrontZ] = legacy.DirFront[0], legacy.DirFront[1], legacy.DirFront[2]
	row[IdxUpX], row[IdxUpY], row[IdxUpZ] = legacy.DirUp[0], legacy.DirUp[1], legacy.DirUp[2]
	row[IdxSpeed] = floatOr(legacy.Speed, 0)
	return row, true
}

// buildChannels decodes the samples and derives distance, acceleration,
// heading and lateral load, then fills the lap summary.
func buildChannels(lap *Lap, envelope *Envelope) {
	interval := envelope.SampleInterval
	if interval <= 0 {
		interval = lap.SampleInterval
	}
	if interval <= 0 {
		interval = 0.02
	}

	count := len(envelope.Samples)
	channels := &lap.Channels
	hasInputs := false
	rows := make([][sampleColumns]float64, 0, count)
	for index, raw := range envelope.Samples {
		row, ok := decodeRow(raw, index, interval)
		if !ok {
			continue
		}
		if !math.IsNaN(row[IdxThrottle]) || !math.IsNaN(row[IdxBrake]) {
			hasInputs = true
		}
		rows = append(rows, row)
	}
	total := len(rows)
	if total == 0 {
		return
	}

	channels.T = make([]float64, total)
	channels.Dist = make([]float64, total)
	channels.X = make([]float64, total)
	channels.Y = make([]float64, total)
	channels.Z = make([]float64, total)
	channels.Speed = make([]float64, total)
	channels.Accel = make([]float64, total)
	channels.Gradient = make([]float64, total)
	channels.LatG = make([]float64, total)
	channels.Heading = make([]float64, total)
	if hasInputs {
		channels.Throttle = make([]float64, total)
		channels.Brake = make([]float64, total)
		channels.Gear = make([]float64, total)
		channels.Handbrake = make([]float64, total)
		channels.Clutch = make([]float64, total)
	}

	summary := Summary{SampleCount: total, MinSpeed: math.Inf(1)}
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	summary.MinZ, summary.MaxZ = math.Inf(1), math.Inf(-1)
	distance := 0.0
	speedSum := 0.0

	for i, row := range rows {
		x, y, z := row[IdxPosX], row[IdxPosY], nanTo(row[IdxPosZ], 0)
		if i > 0 {
			previous := rows[i-1]
			distance += math.Sqrt(
				square(x-previous[IdxPosX]) +
					square(y-previous[IdxPosY]) +
					square(z-nanTo(previous[IdxPosZ], 0)))
			if z > nanTo(previous[IdxPosZ], 0) {
				summary.ElevationGain += z - nanTo(previous[IdxPosZ], 0)
			}
		}
		speed := nanTo(row[IdxSpeed], 0)
		channels.T[i] = round(row[IdxTime], 4)
		channels.Dist[i] = round(distance, 3)
		channels.X[i] = round(x, 3)
		channels.Y[i] = round(y, 3)
		channels.Z[i] = round(z, 3)
		channels.Speed[i] = round(speed, 4)
		channels.Heading[i] = round(math.Atan2(nanTo(row[IdxFrontY], 0), nanTo(row[IdxFrontX], 1)), 5)
		if hasInputs {
			channels.Throttle[i] = round(nanTo(row[IdxThrottle], 0), 3)
			channels.Brake[i] = round(nanTo(row[IdxBrake], 0), 3)
			channels.Gear[i] = nanTo(row[IdxGear], 0)
			channels.Handbrake[i] = round(nanTo(row[IdxHandbrake], 0), 3)
			channels.Clutch[i] = round(nanTo(row[IdxClutch], 0), 3)
		}

		speedSum += speed
		if speed > summary.TopSpeed {
			summary.TopSpeed = speed
		}
		if speed < summary.MinSpeed {
			summary.MinSpeed = speed
		}
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
		summary.MinZ, summary.MaxZ = math.Min(summary.MinZ, z), math.Max(summary.MaxZ, z)
	}

	deriveAccel(channels)
	deriveLatG(channels)
	deriveGradient(channels)

	summary.Distance = round(distance, 2)
	summary.Duration = channels.T[total-1] - channels.T[0]
	summary.AvgSpeed = round(speedSum/float64(total), 4)
	summary.Bounds = [4]float64{round(minX, 3), round(minY, 3), round(maxX, 3), round(maxY, 3)}
	summary.MinZ, summary.MaxZ = round(summary.MinZ, 2), round(summary.MaxZ, 2)
	summary.ElevationGain = round(summary.ElevationGain, 1)
	if math.IsInf(summary.MinSpeed, 1) {
		summary.MinSpeed = 0
	}
	summary.TopSpeed = round(summary.TopSpeed, 4)
	summary.MinSpeed = round(summary.MinSpeed, 4)

	for _, value := range channels.Accel {
		summary.MaxAccel = math.Max(summary.MaxAccel, value)
		summary.MaxDecel = math.Min(summary.MaxDecel, value)
	}
	for _, value := range channels.LatG {
		summary.MaxLatG = math.Max(summary.MaxLatG, math.Abs(value))
	}
	for _, value := range channels.Gradient {
		summary.MaxGradient = math.Max(summary.MaxGradient, value)
		summary.MinGradient = math.Min(summary.MinGradient, value)
	}
	summary.MaxGradient = round(summary.MaxGradient, 2)
	summary.MinGradient = round(summary.MinGradient, 2)
	summary.MaxAccel = round(summary.MaxAccel, 3)
	summary.MaxDecel = round(summary.MaxDecel, 3)
	summary.MaxLatG = round(summary.MaxLatG, 3)

	if hasInputs {
		throttleSamples, brakeSamples, coastSamples := 0, 0, 0
		for i := range channels.Throttle {
			braking := channels.Brake[i] > 0.05
			throttling := channels.Throttle[i] > 0.95
			if throttling {
				throttleSamples++
			}
			if braking {
				brakeSamples++
			}
			if !braking && channels.Throttle[i] <= 0.05 {
				coastSamples++
			}
		}
		summary.FullThrottlePct = round(100*float64(throttleSamples)/float64(total), 1)
		summary.BrakingPct = round(100*float64(brakeSamples)/float64(total), 1)
		summary.CoastingPct = round(100*float64(coastSamples)/float64(total), 1)
	}

	lap.Summary = summary
	lap.HasInputs = hasInputs
	if lap.Duration == 0 {
		lap.Duration = round(summary.Duration, 3)
	}
}

// deriveAccel is the central difference of speed over time, in m/s².
func deriveAccel(channels *Channels) {
	total := len(channels.Speed)
	for i := 0; i < total; i++ {
		low, high := maxInt(0, i-1), minInt(total-1, i+1)
		dt := channels.T[high] - channels.T[low]
		if dt <= 0 {
			continue
		}
		channels.Accel[i] = round((channels.Speed[high]-channels.Speed[low])/dt, 3)
	}
}

// deriveLatG turns the heading rate into lateral load in g. Heading comes from
// the recorded forward vector, so it is smoothed over a short window before
// differentiating; at 50 Hz the raw rate is dominated by sampling noise.
func deriveLatG(channels *Channels) {
	total := len(channels.Heading)
	const window = 3
	for i := 0; i < total; i++ {
		low, high := maxInt(0, i-window), minInt(total-1, i+window)
		dt := channels.T[high] - channels.T[low]
		if dt <= 0 {
			continue
		}
		delta := wrapAngle(channels.Heading[high] - channels.Heading[low])
		channels.LatG[i] = round(channels.Speed[i]*(delta/dt)/gravity, 3)
	}
}

// deriveGradient is the road's slope as a percentage: metres climbed per 100
// metres travelled, measured over a window in metres so that crawling does not
// turn it into noise.
func deriveGradient(channels *Channels) {
	total := len(channels.Z)
	for i := 0; i < total; i++ {
		low := i
		for low > 0 && channels.Dist[i]-channels.Dist[low] < gradientWindow {
			low--
		}
		high := i
		for high < total-1 && channels.Dist[high]-channels.Dist[i] < gradientWindow {
			high++
		}
		run := channels.Dist[high] - channels.Dist[low]
		if run <= 0 {
			continue
		}
		channels.Gradient[i] = round((channels.Z[high]-channels.Z[low])/run*100, 3)
	}
}

func wrapAngle(value float64) float64 {
	for value > math.Pi {
		value -= 2 * math.Pi
	}
	for value < -math.Pi {
		value += 2 * math.Pi
	}
	return value
}

func square(value float64) float64 { return value * value }

func nanTo(value, fallback float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	return value
}

func round(value float64, digits int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
