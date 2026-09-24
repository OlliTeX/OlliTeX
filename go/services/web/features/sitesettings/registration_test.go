// U9 route + flag pins.
package sitesettings

import (
	"context"
	"testing"

	"ollitex/go/services/web/core"
)

// RegistrationEnabled — env clause (boolFromEnv parity: only the exact
// strings "true"/"false" are conclusive; any other value falls through —
// with no site_settings DB present the SSO test is all-undefined → true).
func TestRegistrationEnabledEnv(t *testing.T) {
	app := &core.App{}
	t.Run("unset", func(t *testing.T) {
		t.Setenv("OVERLEAF_ENABLE_REGISTRATION_PAGE", "")
		if !RegistrationEnabled(app, context.Background()) {
			t.Fatal("no env + no SSO → enabled (Node seed)")
		}
	})
	t.Run("explicit true", func(t *testing.T) {
		t.Setenv("OVERLEAF_ENABLE_REGISTRATION_PAGE", "true")
		if !RegistrationEnabled(app, context.Background()) {
			t.Fatal("env true → enabled")
		}
	})
	t.Run("explicit false", func(t *testing.T) {
		t.Setenv("OVERLEAF_ENABLE_REGISTRATION_PAGE", "false")
		if RegistrationEnabled(app, context.Background()) {
			t.Fatal("env false → disabled")
		}
	})
	t.Run("non-conclusive falls through", func(t *testing.T) {
		t.Setenv("OVERLEAF_ENABLE_REGISTRATION_PAGE", "TRUE") // Node: undefined
		if !RegistrationEnabled(app, context.Background()) {
			t.Fatal("Node boolFromEnv: 'TRUE' → undefined → SSO test (none) → enabled")
		}
	})
}
