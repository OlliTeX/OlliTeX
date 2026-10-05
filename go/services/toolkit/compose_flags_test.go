package toolkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Owner D + E (2026-10-06): the OPTIONAL SERVICE GROUPS live inside the
// canonical toolkit/compose.yaml; the ONE control mechanism is the
// per-group restart flag (restart: "${ENABLE_*:-unless-stopped}", "no" =
// off); GROUPS ARE ACTIVE BY DEFAULT; ALL host ports are env-parameterized
// with NORMAL defaults. Compose PROFILES and the legacy lib overlays are
// retired (owner E 2026-10-06 retired toolkit/bin, doc, lib overlays).

func canonicalComposePath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "..", "toolkit", "compose.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("canonical compose missing: %v", err)
	}
	return p
}

func TestCanonical_OptionalGroupsFlagContract(t *testing.T) {
	tk, _ := offlineToolkit(t)
	tk.ComposeFile = canonicalComposePath(t)
	b, err := os.ReadFile(tk.ComposeFile)
	if err != nil {
		t.Fatalf("read canonical compose: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "profiles:") {
		t.Fatal("canonical compose.yaml must NOT use compose profiles (the ONE mechanism is the ENABLE_* flags)")
	}
	for _, flag := range []string{
		"ENABLE_SEAWEEDFS", "ENABLE_WAKA_API", "ENABLE_LANGUAGE_TOOL", "ENABLE_MONITORING",
	} {
		want := "restart: ${" + flag + ":-unless-stopped}"
		if !strings.Contains(s, want) {
			t.Errorf("missing per-group flag %q (active-by-default restart policy)", flag)
		}
	}
	// active by default, no store key involved: a clean-store Plan renders
	// the single canonical file unharmed (no overlay files — they are
	// retired; the flags are interpreted by docker compose itself).
	plan, err := tk.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Files) != 1 || filepath.Base(plan.Files[0]) != "compose.yaml" {
		t.Fatalf("plan.Files = %v, want exactly the canonical compose.yaml (no overlays)", plan.Files)
	}
}

func TestCanonical_PortEnvelope(t *testing.T) {
	b, err := os.ReadFile(canonicalComposePath(t))
	if err != nil {
		t.Fatalf("read canonical compose: %v", err)
	}
	s := string(b)
	// every host-side port must be env-parameterized with a NORMAL default
	// (dev boxes override in .env — owner 2026-10-06 port-config request)
	for _, want := range []string{
		`${PG_PORT_HOST:-5432}:5432`,
		`${MONGO_PORT_HOST:-27017}:27017`,
		`${REDIS_PORT_HOST:-6379}:6379`,
		`${SSH_PORT:-2222}:2222`,
		`${PROMETHEUS_PORT:-9090}:9090`,
		`${GRAFANA_PORT:-3000}:3000`,
		`${MONGODB_EXPORTER_PORT:-9216}:9216`,
		`${REDIS_EXPORTER_PORT:-9121}:9121`,
		`${NODE_EXPORTER_PORT:-9100}:9100`,
		`${SEAWEEDFS_MASTER_PORT:-9333}:9333`,
		`${SEAWEEDFS_FILER_PORT:-8333}:8333`,
		`${SEAWEEDFS_S3_PORT:-8888}:8888`,
		`${WAKA_API_PORT:-3000}:3000`,
		`${LANGUAGE_TOOL_PORT:-8010}:8010`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("canonical compose.yaml missing env-parameterized port %q", want)
		}
	}
}
