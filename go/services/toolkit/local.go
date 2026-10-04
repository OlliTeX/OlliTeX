package toolkit

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// DoctorRow is one CLI doctor line.
type DoctorRow struct {
	Label  string
	Detail string
	OK     bool
}

// NewAppForLocal exposes the TUI model for local (non-SSH) runs — the same
// model wish sessions get, so dev and deployed behavior stay identical.
func NewAppForLocal(t *Toolkit) tea.Model {
	return newApp(t)
}

// Doctor runs the one-shot health checks (CLI `toolkit doctor`).
func Doctor(ctx context.Context, t *Toolkit) ([]DoctorRow, error) {
	app := newApp(t)
	err := error(nil)
	if app.dock != nil {
		err = app.dock.Ping(ctx)
	}
	if app.dock == nil {
		err = fmt.Errorf("docker client unavailable")
	}
	rows := make([]DoctorRow, 0, 4)
	if app.dock != nil {
		if err == nil {
			rows = append(rows, DoctorRow{Label: "docker socket", Detail: "daemon reachable", OK: true})
		} else {
			rows = append(rows, DoctorRow{Label: "docker socket", Detail: "unreachable: " + err.Error(), OK: false})
		}
	} else {
		rows = append(rows, DoctorRow{Label: "docker socket", Detail: "client unavailable (mount check needed)", OK: false})
	}
	if t.Store != nil {
		if _, kerr := t.Store.Keys(); kerr == nil {
			rows = append(rows, DoctorRow{Label: "config store", Detail: t.StoreDescribe, OK: true})
		} else {
			rows = append(rows, DoctorRow{Label: "config store", Detail: "open/read failed: " + kerr.Error(), OK: false})
		}
	} else {
		rows = append(rows, DoctorRow{Label: "config store", Detail: "not configured (CONFIG_DB_DSN)", OK: false})
	}
	if fi, serr := os.Stat(t.ComposeFile); serr == nil && !fi.IsDir() {
		rows = append(rows, DoctorRow{Label: "compose file", Detail: t.ComposeFile, OK: true})
	} else {
		rows = append(rows, DoctorRow{Label: "compose file", Detail: t.ComposeFile + " (missing)", OK: false})
	}
	if fi, sdir := os.Stat(t.DataDir); sdir == nil && fi.Mode().Perm()&0o200 != 0 {
		rows = append(rows, DoctorRow{Label: "data dir", Detail: t.DataDir, OK: true})
	} else {
		rows = append(rows, DoctorRow{Label: "data dir", Detail: t.DataDir + " (missing/not writable)", OK: false})
	}
	return rows, nil
}
