// Package projecthistory is the public façade of the project-history service
// (the Go 1:1 port of services/project-history). The production bundle
// (appfactory) lives under the service's internal/ tree and is re-exported
// here so the repo-root entrypoint (cmd/project-history) can import it under
// Go's internal-visibility rules, matching the pattern of the other
// go/services/* services.
package projecthistory

import (
	"context"

	"ollitex/go/services/project-history/internal/appfactory"
	"ollitex/go/services/project-history/internal/config"
)

// App — the built service (handler + teardown).
type App = appfactory.App

// Config — the service configuration (vendor settings.defaults 1:1 via
// config.Load).
type Config = config.Config

// Build wires the production bundle and returns a ready-to-serve handler.
func Build(ctx context.Context, cfg *Config) (*App, error) {
	return appfactory.Build(ctx, cfg)
}

// Load is config.Load — the vendor env-var set.
func Load() *Config { return config.Load() }
