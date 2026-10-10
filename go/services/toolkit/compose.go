package toolkit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"ollitex/go/libraries/configschema"
)

// StackPlan is the fully-rendered intent for one `stack up`:
// which compose overlays (in compose merge order) + the env plane
// (interpolated values) — ALL derived from the config store. There are
// no rc files, no .env files on disk beyond the rendered env file, and
// no docker-env fallbacks: the store is the one true source.
type StackPlan struct {
	Project string
	// Files is the ordered -f list (base first, then enabled overlays,
	// then the optional operator override file — compose merges in order,
	// later wins).
	Files []string
	// Env is the rendered interpolation plane (KEY → value).
	Env map[string]string
	// EnvFile is where Env is written for `--env-file` (inside the mounted
	// data dir, so it is inspectable/auditable).
	EnvFile string
	// Profile — a compose PROFILE to activate (owner 2026-10-06: monitoring
	// lives INSIDE the canonical compose.yaml behind profile "monitoring",
	// not as a separate overlay file): --profile <name> on every invocation.
	Profile string
	// Notes is operator-facing provenance (e.g. generated secrets).
	Notes []string
}

// templateDir is the bundled compose template directory (image: /opt/ollitex/
// compose-templates; local dev: toolkit/lib).
func (t *Toolkit) templateDir() string {
	return filepath.Dir(t.ComposeFile)
}

// val returns the stored value for key, else its registry default, else "".
func (t *Toolkit) val(key string) string {
	if t.Store != nil {
		if v, err := t.Store.Get(key); err == nil && v != "" {
			return v
		}
	}
	if p, ok := configschema.Find(key); ok {
		return p.Default
	}
	return ""
}

// boolVal reads a bool from the store (registry default fallback).
func (t *Toolkit) boolVal(key string) bool {
	v := t.val(key)
	if v == "" {
		return false
	}
	b, _ := strconv.ParseBool(v)
	return b
}

// intVal reads an int from the store (registry default fallback).
func (t *Toolkit) intVal(key string) (int, bool) {
	v := t.val(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// Plan renders the stack plan from the store (doctor + Stack screen + the
// compose execs all consume this one structure).
func (t *Toolkit) Plan() (*StackPlan, error) {
	plan := &StackPlan{
		Project: t.Project,
		Env:     map[string]string{},
		EnvFile: filepath.Join(t.DataDir, "toolkit.env"),
	}

	td := t.templateDir()
	base := t.baseTemplatePath()
	if base == "" {
		return nil, fmt.Errorf("no compose base template (point OLLITEX_TOOLKIT_COMPOSE_FILE at the base template; image default: %s)", filepath.Join(t.templateDir(), "docker-compose.base.yml"))
	}
	plan.Files = append(plan.Files, base)
	// (the legacy lib overlay path is retired with its files — owner E
	// 2026-10-06; the canonical compose.yaml is the single stack file)
	_ = td


	// ---- base plane ------------------------------------------------------
	if v := t.val("OVERLEAF_LISTEN_IP"); v != "" {
		plan.Env["OVERLEAF_LISTEN_IP"] = v
	} else {
		plan.Env["OVERLEAF_LISTEN_IP"] = "0.0.0.0"
		plan.Notes = append(plan.Notes, "OVERLEAF_LISTEN_IP unset — defaulting to 0.0.0.0 (set it in the store for a bound instance)")
	}
	plan.Env["OVERLEAF_PORT"] = t.val("OVERLEAF_PORT")
	plan.Env["OVERLEAF_IN_CONTAINER_DATA_PATH"] = "/var/lib/overleaf"

	dataPath := t.val("OVERLEAF_DATA_PATH")
	if dataPath == "" {
		dataPath = "data/ollitex"
	}
	if !filepath.IsAbs(dataPath) {
		// relative to the mounted data dir (the host folder the operator
		// mounted; the container bind-mounts a host path).
		dataPath = filepath.Join(t.DataDir, dataPath)
	}
	plan.Env["OVERLEAF_DATA_PATH"] = dataPath

	// app image (one true source: OLLITEX_IMAGE, else ollitex/ollitex:$IMAGE_VERSION)
	ollitexImage := t.val("OLLITEX_IMAGE")
	if ollitexImage == "" {
		iv := t.val("IMAGE_VERSION")
		if iv != "" {
			ollitexImage = "ollitex/ollitex:" + iv
		}
	}
	if ollitexImage == "" {
		return nil, fmt.Errorf("no app image: set OLLITEX_IMAGE or IMAGE_VERSION in the config store")
	}
	plan.Env["IMAGE"] = ollitexImage

	// retracted-version guard (transcribed from the toolkit; owner air-gap
	// override honored).
	if iv := t.val("IMAGE_VERSION"); iv == "5.0.1" || strings.HasPrefix(iv, "5.0.1-") {
		skip := t.val("OVERLEAF_SKIP_RETRACTION_CHECK")
		if skip == iv || skip == "5.0.1" {
			plan.Notes = append(plan.Notes, "retracted-version guard skipped (OVERLEAF_SKIP_RETRACTION_CHECK="+skip+")")
		} else {
			return nil, fmt.Errorf("image version 5.0.1 is RETRACTED (known history-migration data-loss bug) — upgrade; set OVERLEAF_SKIP_RETRACTION_CHECK only for air-gapped installs")
		}
	}

	// shared service endpoints (store plane)
	if v := t.val("MONGO_URL"); v != "" {
		plan.Env["MONGO_URL"] = v
	}
	if v := t.val("REDIS_HOST"); v != "" {
		plan.Env["REDIS_HOST"] = v
	}
	if v := t.val("REDIS_PORT"); v != "" {
		plan.Env["REDIS_PORT"] = v
	}

	// ---- overlays (selection flags live in the store) --------------------
	if t.boolVal("REDIS_ENABLED") {
		plan.Env["REDIS_IMAGE"] = t.val("REDIS_IMAGE")
		rp := t.val("REDIS_DATA_PATH")
		if rp != "" {
			plan.Env["REDIS_DATA_PATH"] = absData(t.DataDir, rp)
		}
		if t.boolVal("REDIS_AOF_PERSISTENCE") {
			plan.Env["REDIS_COMMAND"] = "redis-server --appendonly yes --save 60 1 --loglevel warning"
		} else {
			plan.Env["REDIS_COMMAND"] = "redis-server"
		}
	}

	if t.boolVal("MONGO_ENABLED") {
		plan.Env["MONGO_DOCKER_IMAGE"] = t.val("MONGO_IMAGE")
		mp := t.val("MONGO_DATA_PATH")
		if mp != "" {
			plan.Env["MONGO_DATA_PATH"] = absData(t.DataDir, mp)
		}
		major, _ := t.intVal("MONGO_VERSION")
		if major >= 6 {
			plan.Env["MONGOSH"] = "mongosh"
		} else {
			plan.Env["MONGOSH"] = "mongo"
		}
		if major >= 4 {
			plan.Env["MONGO_ARGS"] = "--replSet overleaf"
		} else {
			plan.Env["MONGO_ARGS"] = ""
		}
	}

	if t.boolVal("POSTGRES_ENABLED") {
		plan.Env["POSTGRES_DOCKER_IMAGE"] = t.val("POSTGRES_IMAGE")
		plan.Env["POSTGRES_USER"] = t.val("POSTGRES_USER")
		plan.Env["POSTGRES_DB"] = t.val("POSTGRES_DB")
		pw := ""
		if t.Store != nil {
			if v, err := t.Store.Get("POSTGRES_PASSWORD"); err == nil {
				pw = v
			}
		}
		if pw == "" {
			// never a committed constant: generate + store (audit-logged note)
			buf := make([]byte, 24)
			if _, err := rand.Read(buf); err != nil {
				return nil, err
			}
			pw = hex.EncodeToString(buf)
			if t.Store != nil {
				if err := t.Store.Set("POSTGRES_PASSWORD", pw, "toolkit:plan-generated"); err != nil {
					return nil, fmt.Errorf("store generated POSTGRES_PASSWORD: %w", err)
				}
			}
			plan.Notes = append(plan.Notes, "generated + stored a new POSTGRES_PASSWORD (set one in the store to choose your own)")
		}
		plan.Env["POSTGRES_PASSWORD"] = pw
		pp := t.val("POSTGRES_DATA_PATH")
		if pp != "" {
			plan.Env["POSTGRES_DATA_PATH"] = absData(t.DataDir, pp)
		}
	}

	if t.boolVal("SIBLING_CONTAINERS_ENABLED") {
		if v := t.val("DOCKER_SOCKET_PATH"); v != "" {
			plan.Env["DOCKER_SOCKET_PATH"] = v
		}
	}

	// ---- app-env contract (the ollitex container's required plane) ---------
	// The image bootstrap HARD-requires OVERLEAF_INVITE_TOKEN_SECRET (the
	// 000_check_missing_secrets gate) and the Go plane needs the session /
	// crypto / JWT / API secrets: all six live in the config store (single
	// source of truth — never in a compose file or an env file on disk by
	// hand). Unset secrets are generated + stored (same provenance pattern
	// as POSTGRES_PASSWORD / GRAFANA_ADMIN_PASSWORD) and are NEVER printed.
	for _, key := range []string{
		"CRYPTO_RANDOM",
		"WEB_API_PASSWORD",
		"SHARED_SERVICE_TOKEN",
		"OT_JWT_AUTH_KEY",
		"OVERLEAF_SESSION_SECRET",
		"OVERLEAF_INVITE_TOKEN_SECRET",
	} {
		v := t.val(key)
		if v == "" {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err != nil {
				return nil, err
			}
			v = hex.EncodeToString(buf)
			if t.Store != nil {
				if err := t.Store.Set(key, v, "toolkit:plan-generated"); err != nil {
					return nil, fmt.Errorf("store generated %s: %w", key, err)
				}
			}
			plan.Notes = append(plan.Notes, "generated + stored a new "+key+" (set one in the store to choose your own)")
		}
		plan.Env[key] = v
	}
	// The app's Mongo DSN (and the app-plane alias): the bootstrap + the
	// Node migration both default to `mongodb://dockerhost/sharelatex` when
	// unset — a host that does not exist in the compose network (live
	// smoke-catch: 900_run_web_migrations died with MongoTopologyClosedError
	// while the DB itself was healthy). The store value wins; the default
	// is the e2e-proven single-node replica-set DSN (RS name "overleaf",
	// auto-initiated by the mongo overlay's healthcheck).
	mongoURL := t.val("MONGO_URL")
	if mongoURL == "" {
		mongoURL = "mongodb://mongo:27017/sharelatex?replicaSet=overleaf"
	}
	plan.Env["MONGO_URL"] = mongoURL
	plan.Env["OVERLEAF_MONGO_URL"] = mongoURL

	// ---- cep-service-set overlays (owner addendum A) --------------------
	if t.boolVal("GITBRIDGE_SERVICE_ENABLED") {
		if v := t.val("GIT_BRIDGE_IMAGE"); v != "" {
			plan.Env["GIT_BRIDGE_IMAGE"] = v
		}
		if v := t.val("GIT_BRIDGE_DATA_PATH"); v != "" {
			plan.Env["GIT_BRIDGE_DATA_PATH"] = absData(t.DataDir, v)
		}
		if v := t.val("GIT_BRIDGE_RUNTIME_JSON"); v != "" {
			plan.Env["GIT_BRIDGE_RUNTIME_JSON"] = absData(t.DataDir, v)
		}
	}
	if t.boolVal("CHECKUSER_ENABLED") {
		if v := t.val("CHECKUSER_IMAGE"); v != "" {
			plan.Env["CHECKUSER_IMAGE"] = v
		}
		if v := t.val("CHECKUSER_DATA_PATH"); v != "" {
			plan.Env["CHECKUSER_DATA_PATH"] = absData(t.DataDir, v)
		}
	}

	if t.boolVal("NGINX_ENABLED") {
		plan.Env["NGINX_IMAGE"] = t.val("NGINX_IMAGE")
		plan.Env["TLS_PORT"] = t.val("TLS_PORT")
		plan.Env["NGINX_HTTP_PORT"] = t.val("NGINX_HTTP_PORT")
		if v := t.val("NGINX_CONFIG_PATH"); v != "" {
			plan.Env["NGINX_CONFIG_PATH"] = v
		}
		if v := t.val("TLS_PRIVATE_KEY_PATH"); v != "" {
			plan.Env["TLS_PRIVATE_KEY_PATH"] = v
		}
		if v := t.val("TLS_CERTIFICATE_PATH"); v != "" {
			plan.Env["TLS_CERTIFICATE_PATH"] = v
		}
		plan.Env["OVERLEAF_TRUSTED_PROXY_IPS"] = t.val("OVERLEAF_TRUSTED_PROXY_IPS")
	}

	// owner D (2026-10-06): ONE mechanism for the optional service groups —
	// the ENABLE_* flags in the canonical compose.yaml (seaweedfs ×4,
	// wakapi+probe, languagetool, monitoring), ACTIVE BY DEFAULT in the
	// file; the admin's start/no-start decision + restart policy is that
	// flag ("no" / "unless-stopped" / "always"). The runner does NOT gate
	// the canonical file: `docker compose up` on it is the complete stack
	// (profiles are out of this design).
	// The legacy lib-base app plane is RETIRED (owner E 2026-10-06: its
	// overlay files, bin/ shell CLI and doc/ went with it) — the canonical
	// compose.yaml is the ONE stack file; the ENABLE_* flags inside it are
	// the ONE group mechanism. No overlay files remain to add.
	canon := filepath.Base(t.ComposeFile) == "compose.yaml"
	_ = canon
	// env plane shared by both planes (the canonical file interpolates these)
	if v := t.val("LANGUAGE_TOOL_IMAGE"); v != "" {
		plan.Env["LANGUAGE_TOOL_IMAGE"] = v
	}
	if v := t.val("LANGUAGE_TOOL_PORT"); v != "" {
		plan.Env["LANGUAGE_TOOL_PORT"] = v
	}
	if v := t.val("LANGUAGE_TOOL_DATA_PATH"); v != "" {
		plan.Env["LANGUAGE_TOOL_DATA_PATH"] = absData(t.DataDir, v)
	}
	if v := t.val("LANGUAGETOOL_URL"); v != "" {
		plan.Env["LANGUAGETOOL_URL"] = v
	}
	for _, k := range []string{"PROMETHEUS_PORT", "GRAFANA_PORT", "PROMETHEUS_IMAGE", "GRAFANA_IMAGE",
		"MONGODB_EXPORTER_IMAGE", "REDIS_EXPORTER_IMAGE", "NODE_EXPORTER_IMAGE", "MONGO_IMAGE",
		"REDIS_IMAGE", "POSTGRES_IMAGE", "SEAWEEDFS_IMAGE", "WAKA_API_IMAGE", "BUSYBOX_IMAGE",
		"WAKA_API_PORT", "HUB_GRAFANA_EMBED_URL", "GRAFANA_ADMIN_PASSWORD",
		"ENABLE_SEAWEEDFS", "ENABLE_WAKA_API", "ENABLE_LANGUAGE_TOOL", "ENABLE_MONITORING"} {
		if v := os.Getenv(k); v != "" {
			plan.Env[k] = v
		} else if dv := t.val(k); dv != "" {
			plan.Env[k] = dv
		}
	}
	if canon && plan.Env["MONITORING_DATA_PATH"] == "" {
		plan.Env["MONITORING_DATA_PATH"] = absData(t.DataDir, "monitoring")
	}	// (wakapi has no legacy overlay — it never had one in the app plane.)

	// ---- monitoring (D22 ecosystem, opt-in) -------------------------
	// Prometheus + the mongo/redis exporters + Grafana — the same image
	// pins as server-ce's d22 profile; scrape config + provisioning +
	// dashboards are materialized into the monitoring data dir (owner
	// policy B: state lives in the mounted data dir) and every container
	// carries a healthcheck (owner policy C).
	// monitoring env plane: the canonical stack is ACTIVE BY DEFAULT (owner
	// D) — render it unconditionally there; the legacy lib plane keeps the
	// MONITORING_ENABLED store-key gate.
	if canon || t.boolVal("MONITORING_ENABLED") {
		// owner 2026-10-06: the monitoring services live INSIDE the canonical
		// compose.yaml (the legacy lib overlay is retained only for the legacy
		// app plane; addOverlay skips absent templates).
	
		if v := t.val("PROMETHEUS_PORT"); v != "" {
			plan.Env["PROMETHEUS_PORT"] = v
		}
		if v := t.val("GRAFANA_PORT"); v != "" {
			plan.Env["GRAFANA_PORT"] = v
		}
		if v := t.val("PROMETHEUS_IMAGE"); v != "" {
			plan.Env["PROMETHEUS_IMAGE"] = v
		}
		if v := t.val("GRAFANA_IMAGE"); v != "" {
			plan.Env["GRAFANA_IMAGE"] = v
		}
		if v := t.val("MONGODB_EXPORTER_IMAGE"); v != "" {
			plan.Env["MONGODB_EXPORTER_IMAGE"] = v
		}
		if v := t.val("REDIS_EXPORTER_IMAGE"); v != "" {
			plan.Env["REDIS_EXPORTER_IMAGE"] = v
		}
		if v := t.val("NODE_EXPORTER_IMAGE"); v != "" {
			plan.Env["NODE_EXPORTER_IMAGE"] = v
		}
		if v := t.val("MONITORING_MONGO_HOST"); v != "" {
			plan.Env["MONITORING_MONGO_HOST"] = v
		}
		if v := t.val("MONITORING_MONGO_PORT"); v != "" {
			plan.Env["MONITORING_MONGO_PORT"] = v
		}
		if v := t.val("MONITORING_REDIS_HOST"); v != "" {
			plan.Env["MONITORING_REDIS_HOST"] = v
		}
		if v := t.val("MONITORING_REDIS_PORT"); v != "" {
			plan.Env["MONITORING_REDIS_PORT"] = v
		}
		mp := t.val("MONITORING_DATA_PATH")
		if mp == "" {
			mp = "data/monitoring"
		}
		plan.Env["MONITORING_DATA_PATH"] = absData(t.DataDir, mp)
		pw := t.val("GRAFANA_ADMIN_PASSWORD")
		if pw == "" {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err != nil {
				return nil, err
			}
			pw = hex.EncodeToString(buf)
			if t.Store != nil {
				if err := t.Store.Set("GRAFANA_ADMIN_PASSWORD", pw, "toolkit:plan-generated"); err != nil {
					return nil, fmt.Errorf("store generated GRAFANA_ADMIN_PASSWORD: %w", err)
				}
			}
			plan.Notes = append(plan.Notes, "generated + stored a new GRAFANA_ADMIN_PASSWORD (set one in the store to choose your own)")
			// ---- hub kiosk opt-in (server-ce/grafana README: explicit opt-in) ---
			// HUB_GRAFANA_EMBED_URL → the app's env (the hub pane reads it);
			// GRAFANA_ANONYMOUS / GRAFANA_CSP_FRAME_ANCESTORS → Grafana's env
			// (kiosk embeds without a login prompt, framed only by the allowed
			// origin — both OFF/default by design: with Grafana still requiring
			// admin auth, a cross-origin iframe of it is a credential-phishing
			// surface, so the embed trio is opt-in all the way down).
			if u := t.val("HUB_GRAFANA_EMBED_URL"); u != "" {
				plan.Env["HUB_GRAFANA_EMBED_URL"] = u
			}
			anon := "false"
			if t.boolVal("GRAFANA_ANONYMOUS") {
				anon = "true"
			}
			plan.Env["GRAFANA_ANONYMOUS"] = anon
			csp := t.val("GRAFANA_CSP_FRAME_ANCESTORS")
			if csp == "" {
				csp = "self"
			}
			plan.Env["GRAFANA_CSP_FRAME_ANCESTORS"] = csp

		}
		plan.Env["GRAFANA_ADMIN_PASSWORD"] = pw
		if err := t.materializeMonitoring(plan.Env["MONITORING_DATA_PATH"]); err != nil {
			return nil, err
		}
	}

	// operator override file (last = highest precedence), if present
	ovr := filepath.Join(t.DataDir, "docker-compose.override.yml")
	if fi, err := os.Stat(ovr); err == nil && !fi.IsDir() {
		plan.Files = append(plan.Files, ovr)
		plan.Notes = append(plan.Notes, "operator override active: "+ovr)
	}
	return plan, nil
}

// baseTemplatePath — the bundled base template (env flag wins).
func (t *Toolkit) baseTemplatePath() string {
	if t.ComposeFile != "" {
		if fi, err := os.Stat(t.ComposeFile); err == nil && !fi.IsDir() {
			return t.ComposeFile
		}
	}
	p := filepath.Join(t.templateDir(), "docker-compose.base.yml")
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return ""
}

// RenderEnvFile writes the plan's env plane (KEY=VALUE, shell-safe quoted)
// into the mounted data dir — inspectable + auditable, and consumed by
// `compose --env-file`.
func (t *Toolkit) RenderEnvFile(plan *StackPlan) error {
	var b strings.Builder
	b.WriteString("# OlliTeX toolkit — rendered compose env plane\n")
	b.WriteString("# SOURCE: the config store (single source of truth). This file is\n")
	b.WriteString("# a render artifact for compose interpolation — DO NOT EDIT by hand;\n")
	b.WriteString("# change the values through the toolkit TUI / configdb / /hub.\n")
	keys := make([]string, 0, len(plan.Env))
	for k := range plan.Env {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		v := plan.Env[k]
		b.WriteString(k + "=" + shellQuote(v) + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(plan.EnvFile), 0o700); err != nil {
		return err
	}
	return os.WriteFile(plan.EnvFile, []byte(b.String()), 0o600)
}

// composeArgs renders the exact CLI args for one compose invocation over the
// plan (files in merge order + env file + project name).
func (t *Toolkit) composeArgs(plan *StackPlan, args ...string) []string {
	out := []string{"compose"}
	for _, f := range plan.Files {
		out = append(out, "-f", f)
	}
	out = append(out, "--project-name", plan.Project)
	if plan.Profile != "" {
		out = append(out, "--profile", plan.Profile)
	}
	if plan.EnvFile != "" {
		out = append(out, "--env-file", plan.EnvFile)
	}
	return append(out, args...)
}

func absData(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// materializeMonitoring — copy the bundled monitoring templates into the
// monitoring data dir (idempotent; renders the scrape-config host
// placeholders). Layout (matching the overlay's mounts):
//
//	<monitoring>/prometheus.yml            ← monitoring/prometheus/prometheus.yml
//	<monitoring>/prometheus-rules.yml       ← monitoring/prometheus/rules/instance-stats.yml (AG alerts)
//	<monitoring>/grafana-provisioning/*    ← monitoring/grafana/provisioning/*
//	<monitoring>/grafana-dashboards/*      ← monitoring/grafana/dashboards/*
func (t *Toolkit) materializeMonitoring(destRoot string) error {
	// owner E 2026-10-06: the monitoring bundles live in toolkit/lib/monitoring
	// (the one lib survivor — bundled into the image at the compose file
	// neighbor path /opt/ollitex/monitoring; the legacy overlay dir is gone).
	td := filepath.Join(t.templateDir(), "monitoring")
	if fi, err := os.Stat(td); err != nil || !fi.IsDir() {
		// local-build fallbacks (repo layouts): toolkit/lib/monitoring
		var rel string
		for _, c := range []string{"toolkit/lib/monitoring", "../../../toolkit/lib/monitoring"} {
			if fi, err := os.Stat(c); err == nil && fi.IsDir() {
				rel = c
				break
			}
		}
		if rel == "" {
			return fmt.Errorf("monitoring templates missing (the toolkit image bundles them — local builds need toolkit/lib/monitoring)")
		}
		td = rel
	}
	// 1) the scrape config (rendered).
	src := filepath.Join(td, "prometheus", "prometheus.yml")
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	yml := strings.ReplaceAll(string(raw), "__OVERLEAF_HOST__", "ollitex")
	yml = strings.ReplaceAll(yml, "__GITBRIDGE_HOST__", "git-bridge")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destRoot, "prometheus.yml"), []byte(yml), 0o644); err != nil {
		return err
	}
	// AG (owner 2026-10-09): the alert rules file (disk/RAM thresholds →
	// the OlliTeX /internal/alerts webhook → email). Rendered with the
	// same host placeholders as the scrape config.
	if rulesRaw, rerr := os.ReadFile(filepath.Join(td, "prometheus", "rules", "instance-stats.yml")); rerr == nil {
		rules := strings.ReplaceAll(string(rulesRaw), "__OVERLEAF_HOST__", "ollitex")
		if werr := os.WriteFile(filepath.Join(destRoot, "prometheus-rules.yml"), []byte(rules), 0o644); werr != nil {
			return werr
		}
	}
	// (the rules file is optional — pre-AG repo layouts are skipped silently)
	// 2) grafana provisioning + dashboards (tree copies).
	pairs := []struct{ src, dst string }{
		{filepath.Join(td, "grafana", "provisioning"), filepath.Join(destRoot, "grafana-provisioning")},
		{filepath.Join(td, "grafana", "dashboards"), filepath.Join(destRoot, "grafana-dashboards")},
	}
	for _, pair := range pairs {
		if err := copyTree(pair.src, pair.dst); err != nil {
			return err
		}
	}
	// 3) stateful dirs pre-created with the image user's ownership — the
	// bind mount (owner policy B) would otherwise be a root-owned dir that
	// the container user cannot write (grafana runs as uid 472;
	// prometheus as uid 65534 nobody) and the container crash-loops.
	if runtime.GOOS == "linux" {
		if err := os.MkdirAll(filepath.Join(destRoot, "grafana-data"), 0o775); err != nil {
			return err
		}
		if err := os.Chown(filepath.Join(destRoot, "grafana-data"), 472, 472); err != nil {
			return fmt.Errorf("chown grafana-data to uid 472 (grafana image user): %w", err)
		}
		if err := os.MkdirAll(filepath.Join(destRoot, "prometheus-data"), 0o775); err != nil {
			return err
		}
		if err := os.Chown(filepath.Join(destRoot, "prometheus-data"), 65534, 65534); err != nil {
			return fmt.Errorf("chown prometheus-data to uid 65534 (prometheus image user): %w", err)
		}
	}
	return nil
}

// copyTree — best-effort recursive copy (files 0644, dirs 0755).
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func shellQuote(v string) string {
	if v == "" {
		return `"` + `"`
	}
	needs := false
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("_./:=+-", r):
		default:
			needs = true
		}
	}
	if needs {
		return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
	}
	return v
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
