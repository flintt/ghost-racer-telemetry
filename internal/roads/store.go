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

type cacheFile struct {
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
	return &file
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, cached: map[string]*Level{}}
}

// Load returns a level's roads, extracting them the first time and reusing the
// cache afterwards. The cache is keyed on the archive's size and timestamp, so a
// game update re-extracts on its own.
func (s *Store) Load(gameRoot, level string) (*Level, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hit, ok := s.cached[level]; ok {
		return hit, nil
	}

	cachePath := filepath.Join(s.dir, level+".json")
	cached := readCache(cachePath)

	archive, err := FindArchive(gameRoot, level)
	if err != nil {
		// No game install reachable: an extraction from an earlier run is still
		// perfectly good road geometry.
		if cached != nil {
			s.cached[level] = cached.Level
			return cached.Level, nil
		}
		return nil, err
	}
	stat, err := os.Stat(archive)
	if err != nil {
		if cached != nil {
			s.cached[level] = cached.Level
			return cached.Level, nil
		}
		return nil, err
	}
	if cached != nil && cached.ArchiveSize == stat.Size() && cached.ArchiveTime.Equal(stat.ModTime()) {
		s.cached[level] = cached.Level
		return cached.Level, nil
	}

	extracted, err := Extract(archive, level)
	if err != nil {
		return nil, err
	}
	s.cached[level] = extracted

	if err := os.MkdirAll(s.dir, 0o755); err == nil {
		if data, marshalErr := json.Marshal(cacheFile{
			Level:       extracted,
			ArchiveSize: stat.Size(),
			ArchiveTime: stat.ModTime(),
		}); marshalErr == nil {
			temp := cachePath + ".tmp"
			if os.WriteFile(temp, data, 0o644) == nil {
				_ = os.Rename(temp, cachePath)
			}
		}
	}
	return extracted, nil
}
