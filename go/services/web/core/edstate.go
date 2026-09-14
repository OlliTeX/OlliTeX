package core

import (
	"os"
	"sync"
)

// EditorGate mirrors the Node process-level Settings.editorIsOpen
// (settings.defaults.js + AdminController.openEditor/closeEditor, which
// mutate the live Settings object at runtime).
//
// Node semantics (pinned live 2026-09-13, P3.1):
//
//	initial:                 process.env.EDITOR_OPEN !== 'false'
//	POST /admin/openEditor:  Settings.editorIsOpen = true
//	POST /admin/closeEditor: Settings.editorIsOpen = body.isOpen
//	                          (UNDEFINED when the field is missing — the
//	                           admin form never sends it)
//	GET  /admin/editor-state: Settings.editorIsOpen !== false  (undefined → true)
//	GET  /status:             !Settings.editorIsOpen           (undefined → closed)
//
// The two readers therefore DIVERGE for the undefined state — the tri-state
// below preserves that. Both Node and Go serve from shared state per
// process; the flip gate compares leg-by-leg (each leg is one process), so
// in-memory state is the right model.
var editorGate = struct {
	mu     sync.Mutex
	isOpen *bool // nil = Node's `undefined`
}{
	isOpen: func() *bool { b := envOpen(); return &b }(),
}

// envOpen mirrors `process.env.EDITOR_OPEN !== 'false'`.
func envOpen() bool { return os.Getenv("EDITOR_OPEN") != "false" }

// EditorOpen mirrors `Settings.editorIsOpen !== false` (the /admin/editor-
// state shape): true unless EXPLICITLY set to false.
func EditorOpen() bool {
	editorGate.mu.Lock()
	defer editorGate.mu.Unlock()
	return editorGate.isOpen == nil || *editorGate.isOpen
}

// EditorClosed mirrors `!Settings.editorIsOpen` (the /status shape):
// false and undefined both count as closed.
func EditorClosed() bool {
	editorGate.mu.Lock()
	defer editorGate.mu.Unlock()
	return editorGate.isOpen == nil || !*editorGate.isOpen
}

// SetEditorOpen implements openEditor(true) / closeEditor(body.isOpen):
// present=false stores Node's `undefined`.
func SetEditorOpen(value bool, present bool) {
	editorGate.mu.Lock()
	defer editorGate.mu.Unlock()
	if present {
		b := value
		editorGate.isOpen = &b
	} else {
		editorGate.isOpen = nil
	}
}

// SiteOpen mirrors Settings.siteIsOpen — `SITE_OPEN !== 'false'` at boot
// (stored site_settings hydration of this flag is P5 scope; no route in
// the P3.1 flip unit can change it).
func SiteOpen() bool { return os.Getenv("SITE_OPEN") != "false" }
