package enforcement

import (
	"sync"
	"time"
)

// InMemoryFindingStore is a thread-safe, bounded FindingStore. Findings are
// operational/transient (re-derivable by re-observing), so in-memory is an
// acceptable MVP; swap in a persistent store via the FindingStore interface.
type InMemoryFindingStore struct {
	mu    sync.Mutex
	max   int
	byID  map[string]*Finding
	byKey map[string]string // dedup key (type|reference) -> id
	order []string          // insertion order of ids (oldest first)
}

// NewInMemoryFindingStore creates a store retaining up to max findings.
func NewInMemoryFindingStore(max int) *InMemoryFindingStore {
	if max <= 0 {
		max = 1000
	}
	return &InMemoryFindingStore{
		max:   max,
		byID:  map[string]*Finding{},
		byKey: map[string]string{},
	}
}

func dedupKey(t FindingType, ref string) string { return string(t) + "|" + ref }

// Upsert inserts a finding or, if one already exists for (Type, Reference),
// refreshes its metadata and timestamp (keeping the original ID, status, and
// detected_at).
func (s *InMemoryFindingStore) Upsert(f Finding) Finding {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := dedupKey(f.Type, f.Reference)
	if id, ok := s.byKey[k]; ok {
		ex := s.byID[id]
		ex.Repo, ex.Ref, ex.Author = f.Repo, f.Ref, f.Author
		ex.UpdatedAt = f.UpdatedAt
		return *ex
	}

	cp := f
	s.byID[cp.ID] = &cp
	s.byKey[k] = cp.ID
	s.order = append(s.order, cp.ID)
	s.evictLocked()
	return cp
}

func (s *InMemoryFindingStore) evictLocked() {
	for len(s.order) > s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		if f, ok := s.byID[oldest]; ok {
			delete(s.byKey, dedupKey(f.Type, f.Reference))
			delete(s.byID, oldest)
		}
	}
}

// List returns findings newest-first, optionally filtered by status ("" = all).
func (s *InMemoryFindingStore) List(status FindingStatus) []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := []Finding{}
	for i := len(s.order) - 1; i >= 0; i-- {
		f := s.byID[s.order[i]]
		if f == nil {
			continue
		}
		if status == "" || f.Status == status {
			out = append(out, *f)
		}
	}
	return out
}

// Get returns a finding by ID.
func (s *InMemoryFindingStore) Get(id string) (Finding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.byID[id]; ok {
		return *f, true
	}
	return Finding{}, false
}

// SetStatus updates a finding's status.
func (s *InMemoryFindingStore) SetStatus(id string, status FindingStatus) (Finding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.byID[id]; ok {
		f.Status = status
		f.UpdatedAt = time.Now()
		return *f, true
	}
	return Finding{}, false
}
