// Package denylist implements the logical-delete deny-list: a per-index set
// of enVector item_ids that have been deleted. Vault is the single source of
// truth; clients consult it (FilterDeleted) and filter out deleted hits.
// Vault never talks to enVector and never filters scores itself.
package denylist

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const persistDebounce = 100 * time.Millisecond

// entry is the in-memory deny-list for a single index: the set of deleted
// item_ids plus a monotonic version that increments on every mutation.
type entry struct {
	set     map[uint64]struct{}
	version uint64
}

// Store is a file-backed, debounce-persisted deny-list keyed by index name.
// Concurrency and persistence mirror tokens.Store.
type Store struct {
	mu      sync.RWMutex
	byIndex map[string]*entry
	path    string

	persistMu     sync.Mutex
	persistTimer  *time.Timer
	persistWG     sync.WaitGroup
	persistClosed bool
}

// NewStore returns an empty, unpersisted deny-list store. Call LoadFromFile to
// back it with a YAML file and enable persistence.
func NewStore() *Store {
	return &Store{byIndex: make(map[string]*entry)}
}

// fileDoc is the on-disk YAML shape: a map of index name -> {item_ids, version}.
type fileDoc struct {
	Indexes map[string]indexDoc `yaml:"indexes"`
}

type indexDoc struct {
	ItemIDs []uint64 `yaml:"item_ids"`
	Version uint64   `yaml:"version"`
}

// LoadFromFile reads the deny-list from YAML at startup and enables persistence
// to path. A missing file is not an error: the store starts empty.
func (s *Store) LoadFromFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read deny-list file %s: %w", path, err)
	}
	var doc fileDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse deny-list file %s: %w", path, err)
	}
	for name, idx := range doc.Indexes {
		set := make(map[uint64]struct{}, len(idx.ItemIDs))
		for _, id := range idx.ItemIDs {
			set[id] = struct{}{}
		}
		s.byIndex[name] = &entry{set: set, version: idx.Version}
	}
	return nil
}

// MarkDeleted unions itemIDs into the deny-list for index and bumps its
// version. It is idempotent: re-marking already-deleted ids still bumps the
// version (a mutation was requested) but does not change membership. Returns
// the post-union deny-list size and the new version.
func (s *Store) MarkDeleted(index string, itemIDs []uint64) (count, version uint64) {
	s.mu.Lock()
	e, ok := s.byIndex[index]
	if !ok {
		e = &entry{set: make(map[uint64]struct{}, len(itemIDs))}
		s.byIndex[index] = e
	}
	for _, id := range itemIDs {
		e.set[id] = struct{}{}
	}
	e.version++
	count = uint64(len(e.set))
	version = e.version
	s.mu.Unlock()
	s.schedulePersist()
	return count, version
}

// FilterDeleted returns the subset of itemIDs that is on the deny-list for
// index, plus the index's current version. Cost is O(len(itemIDs)) and
// independent of the total deny-list size. Unknown index returns an empty
// subset and version 0.
func (s *Store) FilterDeleted(index string, itemIDs []uint64) (deleted []uint64, version uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byIndex[index]
	if !ok {
		return nil, 0
	}
	for _, id := range itemIDs {
		if _, found := e.set[id]; found {
			deleted = append(deleted, id)
		}
	}
	return deleted, e.version
}

// Shutdown cancels any pending persist and waits for in-flight writes.
// Use Flush instead to write pending changes before exit.
func (s *Store) Shutdown() {
	s.persistMu.Lock()
	s.persistClosed = true
	if s.persistTimer != nil {
		s.persistTimer.Stop()
		s.persistTimer = nil
	}
	s.persistMu.Unlock()
	s.persistWG.Wait()
}

// Flush forces any pending debounced persist to run synchronously, then blocks
// until in-flight writes complete.
func (s *Store) Flush() {
	s.persistMu.Lock()
	pending := false
	if s.persistTimer != nil {
		if s.persistTimer.Stop() {
			pending = true
		}
		s.persistTimer = nil
	}
	s.persistMu.Unlock()
	if pending {
		s.doPersist()
	}
	s.persistWG.Wait()
}

func (s *Store) schedulePersist() {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if s.persistClosed || s.path == "" {
		return
	}
	if s.persistTimer != nil {
		s.persistTimer.Stop()
	}
	s.persistTimer = time.AfterFunc(persistDebounce, func() {
		s.persistMu.Lock()
		s.persistTimer = nil
		closed := s.persistClosed
		s.persistMu.Unlock()
		if closed {
			return
		}
		s.doPersist()
	})
}

func (s *Store) doPersist() {
	s.persistWG.Add(1)
	defer s.persistWG.Done()

	s.mu.RLock()
	path := s.path
	doc := fileDoc{Indexes: make(map[string]indexDoc, len(s.byIndex))}
	for name, e := range s.byIndex {
		ids := make([]uint64, 0, len(e.set))
		for id := range e.set {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		doc.Indexes[name] = indexDoc{ItemIDs: ids, Version: e.version}
	}
	s.mu.RUnlock()

	if err := atomicWriteYAML(path, doc); err != nil {
		fmt.Fprintf(os.Stderr, "denylist: persist failed: %v\n", err)
	}
}

func atomicWriteYAML(path string, data any) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".persist-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	enc := yaml.NewEncoder(tmp)
	enc.SetIndent(2)
	if err := enc.Encode(data); err != nil {
		_ = enc.Close()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := enc.Close(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
