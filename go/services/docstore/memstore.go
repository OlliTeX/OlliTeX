package docstore

// memstore.go — in-memory Store for tests and for running the Go docstore
// against Node's database semantics without a live secondary; mirrors the
// Store interface exactly (same success/failure outcomes, same 1:1 rules).

import (
	"context"
	"sort"
	"sync"
	"time"
)

type memStore struct {
	mu    sync.Mutex
	docs  []*Doc
	order map[string]*Doc // "pid/did" → doc
}

func NewMemStore() *memStore {
	return &memStore{order: map[string]*Doc{}}
}

func key(pid, did string) string { return pid + "/" + did }

func cloneTree(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = cloneTree(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneTree(e)
		}
		return out
	case []string:
		out := make([]string, len(t))
		copy(out, t)
		return out
	default:
		return v
	}
}

func cloneDoc(d *Doc) *Doc {
	if d == nil {
		return nil
	}
	out := *d
	if d.Lines != nil {
		l := make([]string, len(*d.Lines))
		copy(l, *d.Lines)
		out.Lines = &l
	}
	if d.Rev != nil {
		r := *d.Rev
		out.Rev = &r
	}
	if d.Version != nil {
		v := *d.Version
		out.Version = &v
	}
	if d.Ranges != nil {
		out.Ranges = cloneTree(d.Ranges)
	}
	if d.Deleted != nil {
		b := *d.Deleted
		out.Deleted = &b
	}
	if d.InS3 != nil {
		b := *d.InS3
		out.InS3 = &b
	}
	return &out
}

func (m *memStore) findDoc(pid, did string) *Doc {
	return m.order[key(pid, did)]
}

// projected returns a view of d with only the projected fields (Node
// projection semantics; _id always present).
func (d *Doc) projected(p docProj) *Doc {
	out := &Doc{ID: d.ID, ProjectID: d.ProjectID}
	if p.Lines {
		l := cloneDoc(d).Lines
		out.Lines = l
	}
	if p.Rev {
		if d.Rev != nil {
			r := *d.Rev
			out.Rev = &r
		}
	}
	if p.Version {
		if d.Version != nil {
			v := *d.Version
			out.Version = &v
		} else {
			z := int64(0)
			out.Version = &z // Node findDoc: !doc.version → 0
		}
	}
	if p.Ranges {
		out.Ranges = cloneDoc(d).Ranges
	}
	if p.Deleted {
		if d.Deleted != nil {
			b := *d.Deleted
			out.Deleted = &b
		}
	}
	if p.InS3 {
		if d.InS3 != nil {
			b := *d.InS3
			out.InS3 = &b
		}
	}
	if p.Name {
		if d.Name != nil {
			s := *d.Name
			out.Name = &s
		}
	}
	if p.DeletedAt {
		if d.DeletedAt != nil {
			ta := *d.DeletedAt
			out.DeletedAt = &ta
		}
	}
	return out
}

func (m *memStore) FindDoc(ctx context.Context, pid, did string, p docProj, useSecondary bool) (*Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.findDoc(pid, did)
	if d == nil {
		return nil, nil
	}
	return d.projected(p), nil
}

func (m *memStore) ProjectDocs(ctx context.Context, pid string, o ProjectDocOpts) ([]*Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	docs := []*Doc{}
	for _, d := range m.docs {
		if d.ProjectID != pid {
			continue
		}
		if o.NonArchivedOnly {
			if b := d.InS3; b != nil && *b {
				continue
			}
		}
		if o.ArchivedOnly {
			if b := d.InS3; b == nil || !*b {
				continue
			}
		}
		if !o.IncludeDeleted {
			// Node getProjectsDocs default: {deleted: {$ne: true}}
			if b := d.Deleted; b != nil && *b {
				continue
			}
		}
		docs = append(docs, d.projected(projOfOpts(o)))
	}
	if o.Limit > 0 && len(docs) > o.Limit {
		docs = docs[:o.Limit]
	}
	return docs, nil
}

func projOfOpts(o ProjectDocOpts) docProj {
	return docProj{Lines: o.WantLines, Rev: o.WantRev, Version: o.WantVersion, Ranges: o.WantRanges}
}

func (m *memStore) DeletedDocs(ctx context.Context, pid string, limit int) ([]*Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	docs := []*Doc{}
	for _, d := range m.docs {
		if d.ProjectID != pid {
			continue
		}
		if b := d.Deleted; b == nil || !*b {
			continue
		}
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool { return afterDeletedAt(docs[i], docs[j]) })
	if limit > 0 && len(docs) > limit {
		docs = docs[:limit]
	}
	out := make([]*Doc, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.projected(docProj{Name: true, DeletedAt: true}))
	}
	return out, nil
}

func deletedAtOf(d *Doc) time.Time {
	if d.DeletedAt != nil {
		return *d.DeletedAt
	}
	return time.Time{}
}

func afterDeletedAt(a, b *Doc) bool {
	return deletedAtOf(a).After(deletedAtOf(b))
}

func (m *memStore) GetDocRev(ctx context.Context, docID string) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.docs {
		if d.ID == docID && d.Rev != nil {
			return *d.Rev, true, nil
		}
	}
	return 0, false, nil
}

func (m *memStore) UpsertDoc(ctx context.Context, pid, did string, previousRev int64, u WriteUpdates) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := m.findDoc(pid, did)
	if previousRev > 0 {
		if existing == nil || existing.Rev == nil || *existing.Rev != previousRev {
			return ErrDocRevValue
		}
		applyWrite(existing, u, previousRev+1)
		return nil
	}
	if existing != nil {
		return ErrDocRevValue // duplicate key
	}
	d := &Doc{ID: did, ProjectID: pid, Rev: int64Ptr(1)}
	applyWrite(d, u, 1)
	m.docs = append(m.docs, d)
	m.order[key(pid, did)] = d
	return nil
}

func int64Ptr(v int64) *int64 { return &v }

func applyWrite(d *Doc, u WriteUpdates, rev int64) {
	if u.Lines != nil {
		l := make([]string, len(*u.Lines))
		copy(l, *u.Lines)
		d.Lines = &l
	}
	if u.Ranges != nil {
		d.Ranges = cloneTree(*u.Ranges)
	}
	if u.Version != nil {
		d.Version = int64Ptr(*u.Version)
	}
	if u.Lines != nil || u.Ranges != nil {
		d.Rev = int64Ptr(rev)
	}
	d.InS3 = nil // $unset inS3
}

func (m *memStore) PatchDocMeta(ctx context.Context, pid, did string, deletedAt time.Time, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.findDoc(pid, did)
	if d == nil {
		return nil // Node UpdateOne: matched 0 is a no-op (route 404s first)
	}
	d.Deleted = boolPtr(true)
	d.DeletedAt = timePtr(deletedAt)
	d.Name = strPtr(name)
	return nil
}

func boolPtr(v bool) *bool           { return &v }
func timePtr(t time.Time) *time.Time { return &t }
func strPtr(s string) *string        { return &s }

func (m *memStore) GetDocForArchiving(ctx context.Context, pid, did string, lockUntil time.Time) (*Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.findDoc(pid, did)
	if d == nil {
		return nil, nil
	}
	if b := d.InS3; b != nil && *b {
		return nil, nil // already archived
	}
	now := time.Now()
	if d.ArchivingUntil != nil && d.ArchivingUntil.After(now) {
		return nil, nil // lock held
	}
	original := d.projected(docProj{Lines: true, Ranges: true, Rev: true})
	d.ArchivingUntil = timePtr(lockUntil)
	return original, nil
}

func (m *memStore) MarkDocAsArchived(ctx context.Context, docID string, rev int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.docs {
		if d.ID != docID {
			continue
		}
		if d.Rev != nil && *d.Rev != rev {
			return nil // Node UpdateOne: no match, no error
		}
		d.InS3 = boolPtr(true)
		d.Lines = nil
		d.Ranges = nil
		d.ArchivingUntil = nil
		return nil
	}
	return nil
}

func (m *memStore) RestoreArchivedDoc(ctx context.Context, pid, did string, lines []string, ranges any, rev int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.findDoc(pid, did)
	if d == nil || d.Rev == nil || *d.Rev != rev {
		return ErrDocRevValue
	}
	l := make([]string, len(lines))
	copy(l, lines)
	d.Lines = &l
	d.Ranges = cloneTree(ranges)
	d.InS3 = nil
	return nil
}

func (m *memStore) DestroyProjectDocs(ctx context.Context, pid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.docs[:0]
	for _, d := range m.docs {
		if d.ProjectID == pid {
			delete(m.order, key(pid, d.ID))
		} else {
			kept = append(kept, d)
		}
	}
	m.docs = kept
	return nil
}

func (m *memStore) DeleteDoc(ctx context.Context, pid, did string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.findDoc(pid, did); d != nil {
		delete(m.order, key(pid, did))
		for i, x := range m.docs {
			if x == d {
				m.docs = append(m.docs[:i], m.docs[i+1:]...)
				break
			}
		}
	}
	return nil
}

// seed is a test helper: create a live (non-archived) doc directly.
func (m *memStore) Seed(pid, did string, lines []string, version int64) {
	ctx := context.Background()
	err := m.UpsertDoc(ctx, pid, did, 0, WriteUpdates{Lines: clonePtr(lines), Version: int64Ptr(version)})
	_ = err
}

func clonePtr(lines []string) *[]string {
	l := make([]string, len(lines))
	copy(l, lines)
	return &l
}
