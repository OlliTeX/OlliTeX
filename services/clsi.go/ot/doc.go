// Package ot is the editor-core apply-side OT runtime (CLSI scope).
//
// It ports exactly the slice of the Node.js overleaf-editor-core runtime that
// CLSI exercises in its "apply changes to a snapshot" pipeline:
//
//	Snapshot.fromRaw → Change.mustFromRaw → snapshot.applyAll →
//	loadFiles('eagre') → snapshot.toRaw.
//
// The port is 1:1 faithful to the Node source in
// libraries/overleaf-editor-core/lib (files: snapshot.js, change.js,
// operation/*.js, file_data/*.js, file_map.js, file.js, range.js, errors.js,
// util.js, safe_pathname.js, blob_store_base.js, blob.js): error swallowing
// semantics, UTF-16 code-unit counting, error message formats and wire
// (raw) shapes.
//
// Wire format is defined by the CLSI snapshot storage contract
// (CLSI_RESOURCE_WRITER_SNAPSHOT_V0): a snapshot is { files, projectVersion?,
// v2DocVersions?, timestamp? } where a file is fileData raw;
// rawChangeOperations are batches of raw ops as consumed here.
package ot
