package toolkit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
	addOverlay := func(name string) {
		p := filepath.Join(td, name)
		plan.Files = append(plan.Files, p)
	}

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
		addOverlay("docker-compose.redis.yml")
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
		addOverlay("docker-compose.mongo.yml")
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
		addOverlay("docker-compose.postgres.yml")
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
		addOverlay("docker-compose.sibling-containers.yml")
		if v := t.val("DOCKER_SOCKET_PATH"); v != "" {
			plan.Env["DOCKER_SOCKET_PATH"] = v
		}
	}

	if t.boolVal("NGINX_ENABLED") {
		addOverlay("docker-compose.nginx.yml")
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

	if t.boolVal("LANGUAGE_TOOL_ENABLED") {
		addOverlay("docker-compose.languagetool.yml")
		plan.Env["LANGUAGE_TOOL_IMAGE"] = t.val("LANGUAGE_TOOL_IMAGE")
		plan.Env["LANGUAGE_TOOL_PORT"] = t.val("LANGUAGE_TOOL_PORT")
		if v := t.val("LANGUAGE_TOOL_DATA_PATH"); v != "" {
			plan.Env["LANGUAGE_TOOL_DATA_PATH"] = absData(t.DataDir, v)
		}
		plan.Env["LANGUAGETOOL_URL"] = t.val("LANGUAGETOOL_URL")
	}

	if t.boolVal("SEAWEEDFS_ENABLED") {
		addOverlay("docker-compose.seaweedfs.yml")
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
