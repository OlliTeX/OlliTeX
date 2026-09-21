// Package otc is a 1:1 Go port of `libraries/overleaf-editor-core` — the
// Overleaf operational-transformation (OT) engine: text/text-range
// transformations (retain/insert/remove), tracked changes and comments,
// file-tree operations and rebasing.
//
// Phase A (this commit) ports the OT text-core + its file_data support, mirroring
// the Node oracle (41 files). Node source → Go:
//
//	errors.js                     → errors.go
//	range.js                      → range.go
//	util.js (containsNonBmpChars) → util.go
//	file_data/tracking_props.js   → tracking.go   (TrackingProps)
//	file_data/clear_tracking_props.js → tracking.go (ClearTrackingProps)
//	operation/scan_op.js          → scan_op.go    (RetainOp/InsertOp/RemoveOp)
//	comment.js                    → comment.go
//	file_data/comment_list.js     → comment_list.go
//	file_data/tracked_change.js   → tracked_change.go
//	file_data/tracked_change_list.go → tracked_change_list.go
//	file_data/string_file_data.js → string_file_data.go
//	operation/text_operation.js   → text_operation.go
//
// Subsequent phases add the file-tree operation family (operation/index,
// no_operation, add/move/edit/set_file_metadata, edit_operation_transformer),
// rebasing, changes, snapshots, and the client/history surface.
//
// Node-platform specifics (Date ↔ timestamp, Buffer.byteLength, Set/Map
// ordering) are expressed with Go idiom; the wire shape (toRaw/fromRaw) and the
// oracle-pinned error messages are preserved exactly.
package otc
