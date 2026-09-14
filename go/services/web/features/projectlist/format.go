package projectlist

// viewModel mirrors ProjectController._buildProjectViewModel (only the fields
// the list consumes: id/name/accessLevel + the per-user archived/trashed
// booleans used by the `.filter(p => !(p.archived || p.trashed))` step).
//
// Node:
//
//	archived = ProjectHelper.isArchived(project, userId)        // userId ∈ archived[]
//	trashed  = ProjectHelper.isTrashed(project, userId) && !archived // userId ∈ trashed[] && !archived
func viewModel(p docRef, accessLevel, uid string) view {
	archived := idIn(p.archived, uid)
	trashed := idIn(p.trashed, uid) && !archived
	return view{
		id:          p.id,
		name:        p.name,
		accessLevel: accessLevel,
		archived:    archived,
		trashed:     trashed,
	}
}

// idIn mirrors `(list || []).some(id => id.equals(userId))`.
func idIn(list []string, uid string) bool {
	for _, x := range list {
		if x == uid {
			return true
		}
	}
	return false
}
