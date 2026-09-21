package otc

// file_tree_diff.go — 1:1 port of `lib/file_tree_diff.js`. Collapses a window
// of changes into one entry per file, keyed by the pathname the file ends up
// at; a chain of moves collapses into a single entry (origin = the pre-window
// pathname). Mirrors FileMap semantics: adding or moving onto an occupied
// pathname replaces the file there, which is then reported as removed.

// FileTreeDiffEntry is a file as tracked through the window (Node
// `FileTreeDiffEntry`).
type FileTreeDiffEntry struct {
	// Origin is the pathname the file had before the window, or nil when the
	// file was added inside the window.
	Origin *string
	// Chain is every pathname the file occupied, in order; the last element is
	// the pathname it ends up at.
	Chain []string
	// Edited reports whether an edit operation touched the file.
	Edited bool
	// FirstEditedAtChainIndex is the chain index of the pathname the file was
	// at when first edited; nil when never edited.
	FirstEditedAtChainIndex *int
	// File is the file added by the most recent add operation; nil when the file
	// was not added inside the window.
	File *File
	// DeletedAtChangeIndex is the index into `changes` of the change that removed
	// the file; nil while the file is part of the tree.
	DeletedAtChangeIndex *int
}

// FileTreeDiff is the result of BuildFileTreeDiff. Entries is keyed by pathname
// (the state of the tree at the end of the window); Order is the insertion
// order (Node Map ordering: initial pathnames first, then those the window
// makes use of). Removed lists every file that left the tree, in the order it
// left.
type FileTreeDiff struct {
	Entries map[string]*FileTreeDiffEntry
	Order   []string
	Removed []*FileTreeDiffEntry
}

// OrderedEntries returns the entries in insertion order.
func (d *FileTreeDiff) OrderedEntries() []*FileTreeDiffEntry {
	out := make([]*FileTreeDiffEntry, 0, len(d.Order))
	for _, p := range d.Order {
		if e, ok := d.Entries[p]; ok {
			out = append(out, e)
		}
	}
	return out
}

// Get returns the entry at a pathname, or nil.
func (d *FileTreeDiff) Get(pathname string) *FileTreeDiffEntry {
	return d.Entries[pathname]
}

// FileTreeDiffOptions mirrors Node's `{initialPathnames, onMoveCollision}`.
// InitialPathnames is "not provided" when nil (any referenced pathname is
// assumed to have existed before the window); a non-nil slice is authoritative.
type FileTreeDiffOptions struct {
	InitialPathnames []string
	OnMoveCollision  func(entry *FileTreeDiffEntry, op *MoveFileOperation)
}

func ftdExistingEntry(pathname string) *FileTreeDiffEntry {
	o := pathname
	return &FileTreeDiffEntry{Origin: &o, Chain: []string{pathname}}
}

func ftdAddedEntry(pathname string, file *File) *FileTreeDiffEntry {
	return &FileTreeDiffEntry{Origin: nil, Chain: []string{pathname}, File: file}
}

func ftdHas(entries map[string]*FileTreeDiffEntry, pathname string) bool {
	_, ok := entries[pathname]
	return ok
}

func ftdRemoveOrder(order []string, pathname string) []string {
	out := make([]string, 0, len(order))
	for _, p := range order {
		if p != pathname {
			out = append(out, p)
		}
	}
	return out
}

// BuildFileTreeDiff — mirroring Node `buildFileTreeDiff(changes, options)`.
func BuildFileTreeDiff(changes []*Change, opts FileTreeDiffOptions) *FileTreeDiff {
	seeded := opts.InitialPathnames != nil
	entries := map[string]*FileTreeDiffEntry{}
	order := []string{}
	removed := []*FileTreeDiffEntry{}

	setEntry := func(pathname string, e *FileTreeDiffEntry) {
		if !ftdHas(entries, pathname) {
			order = append(order, pathname)
		}
		entries[pathname] = e
	}

	if seeded {
		for _, pathname := range opts.InitialPathnames {
			setEntry(pathname, ftdExistingEntry(pathname))
		}
	}

	getLiveEntry := func(pathname string) *FileTreeDiffEntry {
		if e, ok := entries[pathname]; ok && e.DeletedAtChangeIndex == nil {
			return e
		}
		return nil
	}
	removeLiveEntry := func(pathname string, changeIndex int) {
		e := getLiveEntry(pathname)
		if e == nil {
			return
		}
		idx := changeIndex
		e.DeletedAtChangeIndex = &idx
		removed = append(removed, e)
	}

	for changeIndex, change := range changes {
		for _, operation := range change.GetOperations() {
			switch op := operation.(type) {
			case *AddFileOperation:
				pathname := op.GetPathname()
				if pathname == "" {
					continue
				}
				removeLiveEntry(pathname, changeIndex)
				setEntry(pathname, ftdAddedEntry(pathname, op.GetFile()))
			case *EditFileOperation:
				pathname := op.GetPathname()
				if pathname == "" {
					continue
				}
				entry, ok := entries[pathname]
				if !ok {
					entry = ftdExistingEntry(pathname)
					setEntry(pathname, entry)
				} else if entry.DeletedAtChangeIndex != nil {
					// The file has been removed, there is nothing to edit.
					continue
				}
				if !entry.Edited {
					entry.Edited = true
					idx := len(entry.Chain) - 1
					entry.FirstEditedAtChainIndex = &idx
				}
			case *MoveFileOperation:
				pathname := op.GetPathname()
				if pathname == "" {
					continue
				}
				newPathname := op.GetNewPathname()
				if op.IsRemoveFile() {
					if !seeded && !ftdHas(entries, pathname) {
						setEntry(pathname, ftdExistingEntry(pathname))
					}
					removeLiveEntry(pathname, changeIndex)
				} else if pathname != newPathname {
					if target := getLiveEntry(newPathname); target != nil && opts.OnMoveCollision != nil {
						opts.OnMoveCollision(target, op)
					}
					entry := getLiveEntry(pathname)
					if entry == nil {
						// Only a pathname the window has not used can be assumed
						// to have held a file before the window.
						if seeded || ftdHas(entries, pathname) {
							continue
						}
						entry = ftdExistingEntry(pathname)
					}
					removeLiveEntry(newPathname, changeIndex)
					entry.Chain = append(entry.Chain, newPathname)
					setEntry(newPathname, entry)
					delete(entries, pathname)
					order = ftdRemoveOrder(order, pathname)
				}
			}
		}
	}

	return &FileTreeDiff{Entries: entries, Order: order, Removed: removed}
}
