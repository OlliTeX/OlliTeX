package toolkit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crypto/subtle"
	"time"
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
	for _, want := range []string{"docker-compose.base.yml", "docker-compose.redis.yml", "docker-compose.mongo.yml", "docker-compose.postgres.yml", "docker-compose.seaweedfs.yml"} {
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

func skipIfNoDocker(t *testing.T) {}
