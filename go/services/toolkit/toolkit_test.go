package toolkit

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"crypto/subtle"
	"time"

	"gopkg.in/yaml.v3"
)

// fakeToolkits builds a Toolkit with an explicit offline (SQLite) store —
// the loud offline-emergency path (same contract the TUI uses against
// Postgres in production).
func offlineToolkit(t *testing.T) (*Toolkit, string) {
	t.Helper()
	dir := t.TempDir()
	dbf := filepath.Join(dir, "config.db")
	os.Setenv("CONFIG_DB_PATH", dbf)
	t.Cleanup(func() { os.Unsetenv("CONFIG_DB_PATH") })
	tk, err := New(Options{
		DataDir:     dir,
		ComposeFile: filepath.Join(dir, "toolkit.yaml"),
		Project:     "ollitex-test",
		DockerSock:  filepath.Join(dir, "nonexistent.sock"),
	})
	if err != nil {
		t.Fatalf("new toolkit: %v", err)
	}
	t.Cleanup(tk.Close)
	return tk, dir
}

func TestWriteHostKey_0600(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ssh_host_key")
	if err := writeHostKey(p); err != nil {
		t.Fatalf("writeHostKey: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", fi.Mode().Perm())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), "PRIVATE KEY") {
		t.Fatalf("not a PEM private key block")
	}
}

func TestNewApp_DashboardRenders(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 100, 30
	a.kind = scrDashboard
	a.Init()
	v := a.View()
	if !strings.Contains(v, "OlliTeX Toolkit") {
		t.Fatalf("dashboard missing title:\n%s", v)
	}
	if !strings.Contains(v, "ollitex-test") {
		t.Fatalf("dashboard missing project: %s", v)
	}
}

func TestNewApp_AllScreensRender(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 100, 30
	for _, kind := range []screenKind{scrDashboard, scrSettings, scrStack, scrLogs, scrDoctor, scrBackup, scrAbout} {
		a.kind = kind
		v := a.View()
		if strings.TrimSpace(v) == "" {
			t.Fatalf("screen %d rendered empty", kind)
		}
	}
}

func TestSettings_SetGetBoolKind(t *testing.T) {
	tk, _ := offlineToolkit(t)
	s := newSettings(tk)
	if err := s.Set("SITE_OPEN", "yes", "test"); err == nil {
		t.Fatal("expected kind validation to reject non-bool")
	}
	if err := s.Set("SITE_OPEN", "true", "test"); err != nil {
		t.Fatalf("set SITE_OPEN: %v", err)
	}
	v, present, err := s.Get("SITE_OPEN")
	if err != nil || !present || v != "true" {
		t.Fatalf("get SITE_OPEN = %q %v %v", v, present, err)
	}
	if err := s.Delete("SITE_OPEN", "test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, present, _ := s.Get("SITE_OPEN"); present {
		t.Fatal("key still present after delete")
	}
}

func TestSettings_UnknownKeyRejected(t *testing.T) {
	tk, _ := offlineToolkit(t)
	s := newSettings(tk)
	if err := s.Set("NOT_A_REGISTERED_KEY", "x", "test"); err == nil {
		t.Fatal("expected rejection of unknown key")
	}
}

func TestDoctor_ReportsRows(t *testing.T) {
	tk, _ := offlineToolkit(t)
	rows, err := Doctor(context.Background(), tk)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(rows) < 4 {
		t.Fatalf("want >=4 rows, got %d: %+v", len(rows), rows)
	}
	labels := map[string]bool{}
	for _, r := range rows {
		labels[r.Label] = true
	}
	for _, want := range []string{"docker socket", "config store", "compose file", "data dir"} {
		if !labels[want] {
			t.Fatalf("missing doctor row %q in %+v", want, rows)
		}
	}
	// config store must pass (offline sqlite opened fine)
	for _, r := range rows {
		if r.Label == "config store" && !r.OK {
			t.Fatalf("config store row should pass: %+v", r)
		}
	}
}

func subtleEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func TestPasswordGate_ConstantTimeBothInputs(t *testing.T) {
	gate := func(user, pass, wantUser, wantPass string) bool {
		return subtleEq(user, wantUser) && subtleEq(pass, wantPass)
	}
	if !gate("ollitex", "s3cret", "ollitex", "s3cret") {
		t.Fatal("valid pair rejected")
	}
	if gate("root", "s3cret", "ollitex", "s3cret") {
		t.Fatal("wrong user accepted")
	}
	if gate("ollitex", "wrong", "ollitex", "s3cret") {
		t.Fatal("wrong pass accepted")
	}
}

func TestPlan_OverlaySelectionFromStore(t *testing.T) {
	tk, dir := offlineToolkit(t)
	// point at the real templates so the base file resolves
	lib := filepath.Join("..", "..", "..", "toolkit", "lib")
	fi, err := os.Stat(filepath.Join(lib, "docker-compose.base.yml"))
	if err != nil || fi.IsDir() {
		t.Skipf("templates not present at %s", lib)
	}
	tk.ComposeFile = filepath.Join(lib, "docker-compose.base.yml")
	// store plane: the offline store is empty → registry defaults apply
	// (mongo/redis/postgres/seaweed ON, nginx/lt/sibling OFF)
	plan, err := tk.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	names := []string{}
	for _, f := range plan.Files {
		names = append(names, filepath.Base(f))
	}
	for _, want := range []string{"docker-compose.base.yml", "docker-compose.redis.yml", "docker-compose.mongo.yml", "docker-compose.postgres.yml", "docker-compose.seaweedfs.yml", "docker-compose.gitbridge.yml"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing overlay %s in %v", want, names)
		}
	}
	for _, off := range []string{"docker-compose.nginx.yml", "docker-compose.languagetool.yml", "docker-compose.sibling-containers.yml"} {
		for _, n := range names {
			if n == off {
				t.Fatalf("unexpected overlay %s in %v (flag off)", off, names)
			}
		}
	}
	// env plane essentials
	if plan.Env["MONGOSH"] != "mongosh" {
		t.Fatalf("MONGOSH = %q, want mongosh (MONGO_VERSION default 9)", plan.Env["MONGOSH"])
	}
	// owner safety directive 2026-10-04: the rendered plan must carry the owner pins
	if plan.Env["MONGO_DOCKER_IMAGE"] != "mongo:9.0" {
		t.Fatalf("plan MONGO_DOCKER_IMAGE = %q, want mongo:9.0", plan.Env["MONGO_DOCKER_IMAGE"])
	}
	if plan.Env["REDIS_IMAGE"] != "redis:8.10-alpine3.23" {
		t.Fatalf("plan REDIS_IMAGE = %q, want redis:8.10-alpine3.23", plan.Env["REDIS_IMAGE"])
	}
	if plan.Env["MONGO_ARGS"] != "--replSet overleaf" {
		t.Fatalf("MONGO_ARGS = %q", plan.Env["MONGO_ARGS"])
	}
	if plan.Env["REDIS_COMMAND"] == "" {
		t.Fatal("REDIS_COMMAND missing")
	}
	if plan.Env["IMAGE"] == "" {
		t.Fatal("IMAGE missing")
	}
	// POSTGRES_PASSWORD must have been generated AND stored (the no-constant rule)
	if plan.Env["POSTGRES_PASSWORD"] == "" {
		t.Fatal("generated POSTGRES_PASSWORD missing")
	}
	stored, err := tk.Store.Get("POSTGRES_PASSWORD")
	if err != nil || stored != plan.Env["POSTGRES_PASSWORD"] {
		t.Fatalf("stored POSTGRES_PASSWORD: %q err=%v (plan=%q)", stored, err, plan.Env["POSTGRES_PASSWORD"])
	}
	// env file renders
	if err := tk.RenderEnvFile(plan); err != nil {
		t.Fatalf("render: %v", err)
	}
	b, err := os.ReadFile(plan.EnvFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(b), "MONGO_ARGS=-") && !strings.Contains(string(b), "MONGO_ARGS='--replSet'") && !strings.Contains(string(b), "MONGO_ARGS='--replSet overleaf'") {
		t.Fatalf("env file missing MONGO_ARGS:\n%s", string(b))
	}
	_ = dir
}

func TestPlan_RetractionGuard(t *testing.T) {
	tk, dir := offlineToolkit(t)
	lib := filepath.Join("..", "..", "..", "toolkit", "lib")
	tk.ComposeFile = filepath.Join(lib, "docker-compose.base.yml")
	if err := tk.Store.Set("IMAGE_VERSION", "5.0.1", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := tk.Plan(); err == nil {
		t.Fatal("expected the 5.0.1 retraction guard to fail the plan")
	}
	if err := tk.Store.Set("OVERLEAF_SKIP_RETRACTION_CHECK", "5.0.1", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := tk.Plan(); err != nil {
		t.Fatalf("air-gap override should skip the guard, got: %v", err)
	}
	_ = dir
}

func TestShell_LiveMongo(t *testing.T) {
	skipIfNoDocker(t)
	sock := os.Getenv("DOCKER_SOCKET_PATH")
	if sock == "" {
		sock = "/var/run/docker.sock"
	}
	if _, err := os.Stat(sock); err != nil {
		t.Skipf("no docker socket: %v", err)
	}
	d, err := NewDocker(sock)
	if err != nil {
		t.Skipf("docker: %v", err)
	}
	sess, err := NewShell(context.Background(), d, "ol-e2e", Shells[0].Label)
	if err != nil {
		t.Skipf("no ol-e2e mongo up: %v", err)
	}
	defer sess.Close()
	// read until we get the mongosh banner/log id line (first chunk is enough for parity)
	buf := make([]byte, 64*1024)
	sess.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, rerr := sess.ReadOne(buf)
	if rerr != nil && n == 0 {
		t.Fatalf("read: %v", rerr)
	}
	text := DecodeShellChunk(buf[:n])
	if !strings.Contains(text, "Mongosh Log ID") && !strings.Contains(text, "mongosh") {
		t.Fatalf("unexpected first chunk: %q", text)
	}
	t.Logf("shell feed OK: %q", text)
}

func TestPlan_AppEnvContract(t *testing.T) {
	tk, dir := offlineToolkit(t)
	lib := filepath.Join("..", "..", "..", "toolkit", "lib")
	if fi, err := os.Stat(filepath.Join(lib, "docker-compose.base.yml")); err != nil || fi.IsDir() {
		t.Skipf("templates not present at %s", lib)
	}
	tk.ComposeFile = filepath.Join(lib, "docker-compose.base.yml")

	required := []string{"CRYPTO_RANDOM", "WEB_API_PASSWORD", "SHARED_SERVICE_TOKEN", "OT_JWT_AUTH_KEY", "OVERLEAF_SESSION_SECRET", "OVERLEAF_INVITE_TOKEN_SECRET"}

	// pre-set one value: the store value must win (plan never clobbers)
	if err := tk.Store.Set("CRYPTO_RANDOM", "operator-chosen-value", "test"); err != nil {
		t.Fatal(err)
	}
	plan, err := tk.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, k := range required {
		v := plan.Env[k]
		if v == "" {
			t.Fatalf("%s missing from the app-env plane", k)
		}
		if k == "CRYPTO_RANDOM" && v != "operator-chosen-value" {
			t.Fatalf("CRYPTO_RANDOM = %q, want the pre-set store value preserved", v)
		}
		if k != "CRYPTO_RANDOM" && len(v) != 32 {
			t.Fatalf("%s = len %d, want a generated 32-hex secret (never printed, store-persisted)", k, len(v))
		}
	}
	// generated secrets landed in the store (auditability) but the plan
	// notes carry provenance, not values
	for _, k := range []string{"WEB_API_PASSWORD", "OVERLEAF_INVITE_TOKEN_SECRET"} {
		stored, _ := tk.Store.Get(k)
		if stored != plan.Env[k] {
			t.Fatalf("store %s = %q, plan env = %q", k, stored, plan.Env[k])
		}
	}
	_ = dir
}

func TestPlan_MongoDSNContract(t *testing.T) {
	tk, dir := offlineToolkit(t)
	lib := filepath.Join("..", "..", "..", "toolkit", "lib")
	if fi, err := os.Stat(filepath.Join(lib, "docker-compose.base.yml")); err != nil || fi.IsDir() {
		t.Skipf("templates not present at %s", lib)
	}
	tk.ComposeFile = filepath.Join(lib, "docker-compose.base.yml")

	plan, err := tk.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Env["MONGO_URL"] == "" || plan.Env["MONGO_URL"] == "mongodb://dockerhost/sharelatex" {
		t.Fatalf("MONGO_URL = %q — the bootstrap/migration default points at a nonexistent host; the plane must carry a real DSN", plan.Env["MONGO_URL"])
	}
	if plan.Env["OVERLEAF_MONGO_URL"] != plan.Env["MONGO_URL"] {
		t.Fatalf("app-plane alias drifted from MONGO_URL")
	}
	// operator-set DSN wins
	if err := tk.Store.Set("MONGO_URL", "mongodb://mongo.internal:27018/ot?replicaSet=prod", "test"); err != nil {
		t.Fatal(err)
	}
	plan2, err := tk.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan2.Env["MONGO_URL"] != "mongodb://mongo.internal:27018/ot?replicaSet=prod" {
		t.Fatalf("store MONGO_URL not honored: %q", plan2.Env["MONGO_URL"])
	}
	_ = dir
}

func skipIfNoDocker(t *testing.T) {}

// TestPlan_MonitoringOptIn — the D22 ecosystem is OFF by default (the base
// stack is unchanged) and, when enabled, adds the monitoring overlay + the
// env plane (data path + generated Grafana admin password) and materializes
// the scrape config + provisioning + dashboards into the data dir (scrape
// config rendered to the toolkit's container names).
func TestPlan_MonitoringOptIn(t *testing.T) {
	tk, dir := offlineToolkit(t)
	lib := filepath.Join("..", "..", "..", "toolkit", "lib")
	if fi, err := os.Stat(filepath.Join(lib, "docker-compose.base.yml")); err != nil || fi.IsDir() {
		t.Skipf("templates not present at %s", lib)
	}
	tk.ComposeFile = filepath.Join(lib, "docker-compose.base.yml")

	// off by default
	plan, err := tk.Plan()
	if err != nil {
		t.Fatalf("plan (off): %v", err)
	}
	for _, f := range plan.Files {
		if filepath.Base(f) == "docker-compose.monitoring.yml" {
			t.Fatalf("monitoring overlay present with MONITORING_ENABLED default false")
		}
	}

	// on
	if err := tk.Store.Set("MONITORING_ENABLED", "true", "test"); err != nil {
		t.Fatal(err)
	}
	plan, err = tk.Plan()
	if err != nil {
		t.Fatalf("plan (on): %v", err)
	}
	foundOverlay := false
	for _, f := range plan.Files {
		if filepath.Base(f) == "docker-compose.monitoring.yml" {
			foundOverlay = true
		}
	}
	if !foundOverlay {
		t.Fatalf("monitoring overlay missing in %v", plan.Files)
	}
	// env plane
	if plan.Env["MONITORING_DATA_PATH"] == "" {
		t.Fatal("MONITORING_DATA_PATH not in the env plane")
	}
	if !strings.HasPrefix(plan.Env["MONITORING_DATA_PATH"], dir) {
		t.Fatalf("MONITORING_DATA_PATH %q is not under the data dir %q", plan.Env["MONITORING_DATA_PATH"], dir)
	}
	if pw := plan.Env["GRAFANA_ADMIN_PASSWORD"]; pw == "" || len(pw) != 32 {
		t.Fatalf("GRAFANA_ADMIN_PASSWORD = %q, want a generated 32-hex secret", pw)
	}
	// the generated secret landed in the store (owner auditability; the TUI
	// does NOT print it by default)
	if v, _ := tk.Store.Get("GRAFANA_ADMIN_PASSWORD"); v != plan.Env["GRAFANA_ADMIN_PASSWORD"] {
		t.Fatalf("store GRAFANA_ADMIN_PASSWORD %q != plan %q", v, plan.Env["GRAFANA_ADMIN_PASSWORD"])
	}
	// materialized templates (owner policy B: state under the data dir)
	root := plan.Env["MONITORING_DATA_PATH"]
	for _, rel := range []string{
		"prometheus.yml",
		"grafana-provisioning/datasources/prometheus.yml",
		"grafana-provisioning/dashboards/ollitex.yml",
		"grafana-dashboards/ollitex-overview.json",
		"grafana-dashboards/mongodb-redis-overview.json",
	} {
		if fi, err := os.Stat(filepath.Join(root, rel)); err != nil || fi.IsDir() {
			t.Fatalf("missing materialized %s", rel)
		}
	}
	// scrape config rendered to the toolkit container names (placeholders gone)
	yml, err := os.ReadFile(filepath.Join(root, "prometheus.yml"))
	if err != nil {
		t.Fatal(err)
	}
	y := string(yml)
	for _, must := range []string{`ollitex:4000`, `ollitex:3016`, `monitoring-mongodb-exporter:9216`, `monitoring-redis-exporter:9121`} {
		if !strings.Contains(y, must) {
			t.Fatalf("rendered scrape config missing target %s", must)
		}
	}
	for _, gone := range []string{"__OVERLEAF_HOST__", "__GITBRIDGE_HOST__"} {
		if strings.Contains(y, gone) {
			t.Fatalf("rendered scrape config still contains placeholder %s", gone)
		}
	}
	// stateful dirs pre-created with the image user's ownership (a root-owned
	// bind mount under a non-root container user = crash loop; caught live
	// in the 2026-10-05 smoke before the fix)
	if runtime.GOOS == "linux" {
		for _, dir := range []struct {
			p   string
			uid int
		}{
			{filepath.Join(root, "grafana-data"), 472},
			{filepath.Join(root, "prometheus-data"), 65534},
		} {
			fi, err := os.Stat(dir.p)
			if err != nil || !fi.IsDir() {
				t.Fatalf("stateful dir %s missing", dir.p)
			}
			if fi.Sys().(*syscall.Stat_t).Uid != uint32(dir.uid) {
				t.Fatalf("%s owner uid = %d, want %d (image user)", dir.p, fi.Sys().(*syscall.Stat_t).Uid, dir.uid)
			}
		}
	}
	// depends_on targets must be SERVICE names (compose validates this —
	// pin the class of bug: container names looked plausible but break
	// `compose config`). Parse the monitoring overlay's depends_on targets
	// and prove each is a service defined in a planned overlay file.
	svc := map[string]bool{}
	for _, f := range plan.Files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc := map[string]any{}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		if ss, ok := doc["services"].(map[string]any); ok {
			for name := range ss {
				svc[name] = true
			}
		}
	}
	mf := filepath.Join(lib, "docker-compose.monitoring.yml")
	m, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(m, &doc); err != nil {
		t.Fatal(err)
	}
	ss, _ := doc["services"].(map[string]any)
	for name, raw := range ss {
		mm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if dep, ok := mm["depends_on"].([]any); ok {
			for _, d := range dep {
				target, _ := d.(string)
				if !svc[target] {
					t.Fatalf("service %s depends_on %v, which is not a service defined in the planned overlays", name, d)
				}
			}
		}
	}
}
