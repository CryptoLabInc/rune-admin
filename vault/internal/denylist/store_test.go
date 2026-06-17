package denylist

import (
	"path/filepath"
	"sort"
	"testing"
)

func sortedEqual(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]uint64(nil), got...)
	w := append([]uint64(nil), want...)
	sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
	sort.Slice(w, func(i, j int) bool { return w[i] < w[j] })
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

func TestMarkAndFilter(t *testing.T) {
	s := NewStore()
	count, version := s.MarkDeleted("idx", []uint64{1, 2, 3})
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if version != 1 {
		t.Errorf("version = %d, want 1", version)
	}

	deleted, ver := s.FilterDeleted("idx", []uint64{2, 3, 4, 5})
	if !sortedEqual(deleted, []uint64{2, 3}) {
		t.Errorf("deleted = %v, want [2 3]", deleted)
	}
	if ver != 1 {
		t.Errorf("filter version = %d, want 1", ver)
	}
}

func TestMarkIsIdempotentUnion(t *testing.T) {
	s := NewStore()
	s.MarkDeleted("idx", []uint64{1, 2})
	count, version := s.MarkDeleted("idx", []uint64{2, 3})
	if count != 3 {
		t.Errorf("count = %d, want 3 (union {1,2,3})", count)
	}
	if version != 2 {
		t.Errorf("version = %d, want 2 (bumped each mark)", version)
	}
	// Re-marking an already-deleted id keeps membership but still bumps version.
	count, version = s.MarkDeleted("idx", []uint64{1})
	if count != 3 {
		t.Errorf("count = %d, want 3 (no new member)", count)
	}
	if version != 3 {
		t.Errorf("version = %d, want 3", version)
	}
}

func TestFilterUnknownIndex(t *testing.T) {
	s := NewStore()
	deleted, version := s.FilterDeleted("nope", []uint64{1, 2})
	if len(deleted) != 0 {
		t.Errorf("deleted = %v, want empty", deleted)
	}
	if version != 0 {
		t.Errorf("version = %d, want 0", version)
	}
}

func TestPerIndexIsolation(t *testing.T) {
	s := NewStore()
	s.MarkDeleted("a", []uint64{1})
	s.MarkDeleted("b", []uint64{2})
	if d, _ := s.FilterDeleted("a", []uint64{1, 2}); !sortedEqual(d, []uint64{1}) {
		t.Errorf("index a deleted = %v, want [1]", d)
	}
	if d, _ := s.FilterDeleted("b", []uint64{1, 2}); !sortedEqual(d, []uint64{2}) {
		t.Errorf("index b deleted = %v, want [2]", d)
	}
}

func TestPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deny_list.yml")

	s1 := NewStore()
	if err := s1.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	s1.MarkDeleted("idx", []uint64{10, 20, 30})
	s1.MarkDeleted("other", []uint64{99})
	s1.Flush()

	s2 := NewStore()
	if err := s2.LoadFromFile(path); err != nil {
		t.Fatalf("reload LoadFromFile: %v", err)
	}
	deleted, version := s2.FilterDeleted("idx", []uint64{10, 20, 30, 40})
	if !sortedEqual(deleted, []uint64{10, 20, 30}) {
		t.Errorf("reloaded deleted = %v, want [10 20 30]", deleted)
	}
	if version != 1 {
		t.Errorf("reloaded version = %d, want 1", version)
	}
	if d, _ := s2.FilterDeleted("other", []uint64{99}); !sortedEqual(d, []uint64{99}) {
		t.Errorf("reloaded other = %v, want [99]", d)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s := NewStore()
	if err := s.LoadFromFile(filepath.Join(t.TempDir(), "absent.yml")); err != nil {
		t.Fatalf("LoadFromFile on missing file: %v", err)
	}
	if d, _ := s.FilterDeleted("idx", []uint64{1}); len(d) != 0 {
		t.Errorf("deleted = %v, want empty", d)
	}
}
