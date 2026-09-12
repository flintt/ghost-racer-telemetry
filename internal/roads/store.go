package roads

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store keeps extracted levels on disk. Walking a 900 MB archive takes a moment;
// the roads inside it only change when the game is updated.
type Store struct {
	dir   string
	build string

	mu     sync.Mutex
	cached map[string]*Level
}

// cacheFormat is bumped when extraction changes what it produces. It is not
// enough on its own: forgetting to bump it silently keeps an old parse alive,
// which is exactly how MeshRoad bridges and then prefab roads both went on
// missing after support for them was added. The build version is therefore part
// of the key too, so a cache can never outlive the code that wrote it.
const cacheFormat = 3

type cacheFile struct {
	Format      int       `json:"format"`
	Build       string    `json:"build"`
	Level       *Level    `json:"level"`
	ArchiveSize int64     `json:"archiveSize"`
	ArchiveTime time.Time `json:"archiveTime"`
}

func readCache(path, build string) *cacheFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var file cacheFile
	if json.Unmarshal(data, &file) != nil || file.Level == nil {
		return nil
	}
	if file.Format != cacheFormat || file.Build != build {
		return nil
	}
	return &file
}

// NewStore keys its cache on the build as well as the format, so an upgrade
// always re-reads the level rather than trusting whatever the previous binary
// understood.
func NewStore(dir, build string) *Store {
	return &Store{dir: dir, build: build, cached: map[string]*Level{}}
}

// Load returns a level's roads, extracting them the first time and reusing the
// cache afterwards. The cache is keyed on the source's size and timestamp, so a
// game or mod update re-extracts on its own.
func (s *Store) Load(gameRoot, userRoot, level string) (*Level, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hit, ok := s.cached[level]; ok {
		return hit, nil
	}

	cachePath := filepath.Join(s.dir, level+".json")
	cached := readCache(cachePath, s.build)

	source, err := Locate(gameRoot, userRoot, level)
	if err != nil {
		// Nothing reachable to read: an extraction from an earlier run is still
		// perfectly good road geometry.
		if cached != nil {
			s.cached[level] = cached.Level
			return cached.Level, nil
		}
		return nil, err
	}
	size, stamp := fingerprint(source.Description)
	if cached != nil && cached.ArchiveSize == size && cached.ArchiveTime.Equal(stamp) {
		if source.closer != nil {
			_ = source.closer()
		}
		s.cached[level] = cached.Level
		return cached.Level, nil
	}

	extracted, err := Extract(source, level)
	if err != nil {
		return nil, err
	}
	s.cached[level] = extracted

	if err := os.MkdirAll(s.dir, 0o755); err == nil {
		if data, marshalErr := json.Marshal(cacheFile{
			Format:      cacheFormat,
			Build:       s.build,
			Level:       extracted,
			ArchiveSize: size,
			ArchiveTime: stamp,
		}); marshalErr == nil {
			temp := cachePath + ".tmp"
			if os.WriteFile(temp, data, 0o644) == nil {
				_ = os.Rename(temp, cachePath)
			}
		}
	}
	return extracted, nil
}

// fingerprint identifies a source cheaply: an archive by its size and
// timestamp, an unpacked directory by its own timestamp.
func fingerprint(path string) (int64, time.Time) {
	stat, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}
	}
	if stat.IsDir() {
		return 0, stat.ModTime()
	}
	return stat.Size(), stat.ModTime()
}
