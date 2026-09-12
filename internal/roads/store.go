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
	dir string

	mu     sync.Mutex
	cached map[string]*Level
}

// cacheFormat is bumped whenever extraction changes what it produces. Without
// it a cache written by an older build survives — the archive has not changed,
// after all — and the new parsing never runs. That is how MeshRoad bridges went
// on missing after support for them was added.
const cacheFormat = 2

type cacheFile struct {
	Format      int       `json:"format"`
	Level       *Level    `json:"level"`
	ArchiveSize int64     `json:"archiveSize"`
	ArchiveTime time.Time `json:"archiveTime"`
}

func readCache(path string) *cacheFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var file cacheFile
	if json.Unmarshal(data, &file) != nil || file.Level == nil {
		return nil
	}
	if file.Format != cacheFormat {
		return nil
	}
	return &file
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, cached: map[string]*Level{}}
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
	cached := readCache(cachePath)

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
