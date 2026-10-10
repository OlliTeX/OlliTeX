// Python runner gate (owner request H, 2026-10-09 — "Python runner
// settings are missing" on psintern admin-settings).
//
// The editor's Python split-pane (modules/python-runner, Pyodide in a web
// worker) is gated by the `overleaf-code` split-test variant (front-end
// isSplitTestEnabled === "enabled"). That variant was pinned "enabled"
// unconditionally, so the existing admin toggle (misc.pythonRunner) was
// inert. This gate closes the loop:
//
//   - site_settings.global.misc.pythonRunner === false  → runner OFF
//     (editor serves the plain SourceEditor for .py files again);
//   - ENABLE_PYTHON_RUNNER=true|false env wins when conclusive (seed
//     parity, seeds.go);
//   - default ON — the product has always shown the split pane in this
//     stack (the pinned split test), so the default must not regress it;
//     the admin toggle is the explicit rollback path (same pattern as the
//     Typst section: "On by default — the toggle is the rollback path").
package sitesettings

import (
	"context"
	"os"

	"ollitex/go/services/web/core"
)

func PythonRunnerEnabled(a *core.App, ctx context.Context) bool {
	switch os.Getenv("ENABLE_PYTHON_RUNNER") {
	case "true":
		return true
	case "false":
		return false
	}
	if a == nil || a.Mongo == nil {
		return true
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return true
	}
	sections := loadAllSections(ctx, db)
	sec, ok := sections["misc"]
	if !ok {
		return true
	}
	v, jok := ObjGet(sec, "pythonRunner")
	if !jok {
		return true
	}
	b, isB := v.(bool)
	return !isB || b
}
