// Package status implements the /status endpoints of the web app
// (router.mjs publicApiRouter / privateApiRouter /status — the
// plainTextResponse family). Pinned live: 200, text/plain; charset=utf-8,
// X-Content-Type-Options: nosniff, weak ETag, body "web is alive (web)"
// (18 bytes) on the web profile and "web is alive (api)" on the api
// profile.
package status

import (
	"os"
	"strings"

	"ollitex/go/services/web/core"
)

// isAPIProfile mirrors the LoadConfig profile selection (ENABLED_SERVICES
// contains "api" but not "web").
func isAPIProfile() bool {
	s := os.Getenv("ENABLED_SERVICES")
	return strings.Contains(s, "api") && !strings.Contains(s, "web")
}

func statusHandler(cxt *core.Cxt, res *core.Res) {
	if os.Getenv("WEB_GO_SHUTTING_DOWN") == "true" {
		res.SendStatus(503) // "Service Unavailable"
		return
	}
	if os.Getenv("SITE_OPEN") == "false" {
		res.PlainText(200, "web site is closed (web)")
		return
	}
	if os.Getenv("EDITOR_OPEN") == "false" {
		res.PlainText(200, "web editor is closed (web)")
		return
	}
	profile := "web"
	if isAPIProfile() {
		profile = "api"
	}
	res.PlainText(200, "web is alive ("+profile+")")
}

// Feature is the flip unit "status": GET /status on both profiles. Both
// Node registrations are anonymous-public (publicApiRouter /
// privateApiRouter — mounted outside the requireGlobalLogin gate).
var Feature = core.Feature{
	Name: "status",
	Routes: []core.Route{
		{Method: "GET", Path: "/status", NoLogin: true, NoSession: true, Handler: statusHandler},
	},
}
