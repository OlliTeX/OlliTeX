// Package filetreediff mirrors vendor `file_tree_diff.js` (the lib-side fold
// that `FileTreeDiffGenerator.js` wraps). The app-level buildDiff (chunk +
// snapshot → diff entries) is Phase C (needs Chunk/Snapshot); this is the
// pure fold, driven directly by the 456-line vendor oracle
// (`test/unit/file_tree_diff.test.js`).
//
// Collapse a window of file changes into one entry per file, keyed by the
// pathname the file ends up at. Chains of moves collapse into a single entry;
// a replacement (or move onto a live pathname) removes the replaced file,
// reported in `Removed` in the order files left the tree.
package filetreediff

// Op is a file-tree-facing operation (vendor: the operation classes). The
// fold switches on the concrete type; OtherOp (NO_OP, setFileMetadata, …)
// leaves the tree untouched. Marker interface — no methods.
type Op any

// AddOp = vendor AddFileOperation: add a file at Pathname (File is vendor's
// File instance; the fold records it, not inspects it).
type AddOp struct {
	Pathname string
	File     any
}

// EditOp = vendor EditFileOperation: the file at Pathname was edited.
type EditOp struct {
	Pathname string
}

// MoveOp = vendor MoveFileOperation: move a file from Pathname to
// NewPathname. An empty NewPathname is vendor removeFile (isRemoveFile()).
type MoveOp struct {
	Pathname    string
	NewPathname string
}

// IsRemove mirrors vendor MoveFileOperation.isRemoveFile().
func (o *MoveOp) IsRemove() bool { return o.NewPathname == "" }

// OtherOp = vendor NO_OP / setFileMetadata / anything the fold ignores.
type OtherOp struct{}

// Change = vendor Change: an ordered batch of operations.
type Change struct{ ops []Op }

func ChangeOf(ops ...Op) *Change { return &Change{ops: ops} }

func (c *Change) opsList() []Op { return c.ops }

// Entry mirrors vendor FileTreeDiffEntry.
type Entry struct {
	// Origin is the pathname before the window, nil when added inside.
	Origin *string `json:"-"`
	// Chain is every pathname the file occupied, in order.
	Chain []string `json:"-"`
	// Edited is whether an edit operation touched the file.
	Edited bool `json:"-"`
	// FirstEditedAtChainIndex indexes Chain at the first edit; nil when never.
	FirstEditedAtChainIndex *int `json:"-"`
	// File is the file added by the most recent add; nil when not added.
	File any `json:"-"`
	// DeletedAtChangeIndex indexes Changes of the removal; nil while live.
	DeletedAtChangeIndex *int `json:"-"`
}

// Options = vendor { initialPathnames, onMoveCollision }.
type Options struct {
	// InitialPathnames: nil = unseeded (fold assumes pre-window files on
	// demand); non-nil = authoritative seeding (seeded mode).
	InitialPathnames *[]string
	// OnMoveCollision, when non-nil, is called before a move replaces a live
	// target (vendor: for callers that throw on inconsistency — the app
	// throws InconsistentChunkError). A non-nil return aborts the fold.
	OnMoveCollision func(target *Entry, op *MoveOp) error
}

// Result = vendor { entries, removed }. Go's maps are unordered; the vendor
// JS Map iterates in insertion order (which the oracle's
// Array.from(entries,…) iterates), so insertion order is tracked: a key that
// is deleted and re-added re-enters the order at its new position.
type Result struct {
	entries map[string]*Entry
	order   []string
	removed []*Entry
}

// addOrder records the insertion order of a pathname that is about to be set
// (the key must not already be in the map at the point of the call).
func (r *Result) addOrder(pn string) {
	if _, ok := r.entries[pn]; !ok {
		r.order = append(r.order, pn)
	}
}

// Entries returns the entries in insertion order, each keyed by its
// chain-tail pathname (the vendor oracle asserts key == chain tail).
func (r *Result) Entries() []*Entry {
	out := make([]*Entry, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.entries[k])
	}
	return out
}

// Get = r.entries.get(pathname).
func (r *Result) Get(pathname string) *Entry { return r.entries[pathname] }

// Removed = the files that left the tree, in the order they left.
func (r *Result) Removed() []*Entry { return r.removed }

// BuildFileTreeDiff = vendor buildFileTreeDiff.
func BuildFileTreeDiff(changes []*Change, opts *Options) (*Result, error) {
	seeded := opts != nil && opts.InitialPathnames != nil
	res := &Result{entries: map[string]*Entry{}}
	if seeded {
		for _, pn := range *opts.InitialPathnames {
			res.addOrder(pn)
			res.entries[pn] = existingEntry(pn)
		}
	}

	live := func(pathname string) *Entry {
		e := res.entries[pathname]
		if e != nil && e.DeletedAtChangeIndex == nil {
			return e
		}
		return nil
	}
	removeLive := func(pathname string, changeIndex int) {
		e := live(pathname)
		if e == nil {
			return
		}
		e.DeletedAtChangeIndex = intPtr(changeIndex)
		res.removed = append(res.removed, e)
	}

	for changeIndex, change := range changes {
		for _, op := range change.opsList() {
			switch o := op.(type) {
			case *AddOp:
				if o.Pathname == "" {
					continue
				}
				removeLive(o.Pathname, changeIndex)
				res.addOrder(o.Pathname)
				res.entries[o.Pathname] = addedEntry(o.Pathname, o.File)
			case *EditOp:
				pn := o.Pathname
				if pn == "" {
					continue
				}
				entry := res.entries[pn]
				if entry == nil {
					entry = existingEntry(pn)
					res.addOrder(pn)
					res.entries[pn] = entry
				} else if entry.DeletedAtChangeIndex != nil {
					// removed — nothing to edit.
					continue
				}
				if !entry.Edited {
					entry.Edited = true
					first := len(entry.Chain) - 1
					entry.FirstEditedAtChainIndex = &first
				}
			case *MoveOp:
				pn := o.Pathname
				if pn == "" {
					continue
				}
				if o.IsRemove() {
					if !seeded && res.entries[pn] == nil {
						res.addOrder(pn)
						res.entries[pn] = existingEntry(pn)
					}
					removeLive(pn, changeIndex)
					continue
				}
				if pn == o.NewPathname {
					continue
				}
				target := live(o.NewPathname)
				if target != nil && opts.OnMoveCollision != nil {
					if err := opts.OnMoveCollision(target, o); err != nil {
						return nil, err
					}
				}
				entry := live(pn)
				if entry == nil {
					// Only a pathname the window has not made use of can be
					// assumed to have held a file before the window.
					if seeded || res.entries[pn] != nil {
						continue
					}
					entry = existingEntry(pn)
				}
				removeLive(o.NewPathname, changeIndex)
				entry.Chain = append(entry.Chain, o.NewPathname)
				res.addOrder(o.NewPathname)
				res.entries[o.NewPathname] = entry
				delete(res.entries, pn)
				res.order = removeKey(res.order, pn)
			default:
				// OtherOp: no tree change.
			}
		}
	}
	return res, nil
}

func intPtr(v int) *int { return &v }

func removeKey(order []string, k string) []string {
	out := make([]string, 0, len(order))
	for _, x := range order {
		if x != k {
			out = append(out, x)
		}
	}
	return out
}

func existingEntry(pathname string) *Entry {
	origin := pathname
	return &Entry{Origin: &origin, Chain: []string{pathname}}
}

func addedEntry(pathname string, file any) *Entry {
	return &Entry{Chain: []string{pathname}, File: file}
}
