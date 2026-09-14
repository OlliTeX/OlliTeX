package sitesettings

import (
	"os"
	"regexp"
	"strings"
)

// Storage env sync (StorageEnvFile.mjs parity). The managed fragment path
// defaults to /etc/overleaf/env.d/ollitex-storage.sh (OVERLEAF_STORAGE_ENV_FILE
// override). Env lines are ${VAR:-value} so compose env always wins.

// storageEnvDefaultPath — DEFAULT_PATH (process env, read at call time).
func storageEnvDefaultPath() string {
	if v := os.Getenv("OVERLEAF_STORAGE_ENV_FILE"); v != "" {
		return v
	}
	return "/etc/overleaf/env.d/ollitex-storage.sh"
}

const storageEnvHeader =
	"# Managed by OlliTeX admin → Site → Storage (2026-09-14). " +
	"Do not edit by hand — change it from the hub.\n" +
	"# ${VAR:-value} form: explicit container/compose env always wins.\n"

var storageSafe = regexp.MustCompile(`^[A-Za-z0-9_/:=+.\-~]*$`)

func envLine(name, value string) string {
	if value == "" {
		return ""
	}
	if storageSafe.MatchString(value) {
		return "export " + name + "=${" + name + ":-" + value + "}"
	}
	q := strings.ReplaceAll(value, "'", `'\''`)
	return "export " + name + "=${" + name + ":-'" + q + "'}"
}

// buildStorageEnvLines — section → export lines (empty values skipped).
func buildStorageEnvLines(section Obj) []string {
	lines := []string{}
	add := func(name, value string) {
		if l := envLine(name, value); l != "" {
			lines = append(lines, l)
		}
	}
	backend := ""
	switch asStr(ObjGetD(section, "backend")) {
	case "s3":
		backend = "s3"
	case "fs":
		backend = "fs"
	}
	add("OVERLEAF_FILESTORE_BACKEND", backend)
	add("OVERLEAF_FILESTORE_S3_ENDPOINT", asStr(ObjGetD(section, "s3Endpoint")))
	add("OVERLEAF_FILESTORE_S3_ACCESS_KEY_ID", asStr(ObjGetD(section, "s3AccessKeyId")))
	add("OVERLEAF_FILESTORE_S3_SECRET_ACCESS_KEY", asStr(ObjGetD(section, "s3Secret")))
	add("OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME", asStr(ObjGetD(section, "templateFilesBucket")))
	add("OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET", asStr(ObjGetD(section, "projectBlobsBucket")))
	add("OVERLEAF_HISTORY_BLOBS_BUCKET", asStr(ObjGetD(section, "globalBlobsBucket")))
	add("BACKEND", backend)
	add("BUCKET_NAME", asStr(ObjGetD(section, "docstoreArchiveBucket")))
	add("AWS_S3_ENDPOINT", asStr(ObjGetD(section, "s3Endpoint")))
	add("AWS_ACCESS_KEY_ID", asStr(ObjGetD(section, "s3AccessKeyId")))
	add("AWS_SECRET_ACCESS_KEY", asStr(ObjGetD(section, "s3Secret")))
	return lines
}

func renderStorageEnvFile(section Obj) string {
	return storageEnvHeader + strings.Join(buildStorageEnvLines(section), "\n") + "\n"
}

var storageParseRe = regexp.MustCompile(`(?m)^export\s+([A-Z][A-Z0-9_]*)=\$\{[A-Z][A-Z0-9_]*:-([^}]*)\}`)

var storageEnvMap = map[string]string{
	"OVERLEAF_FILESTORE_BACKEND":                    "backend",
	"OVERLEAF_FILESTORE_S3_ENDPOINT":                "s3Endpoint",
	"OVERLEAF_FILESTORE_S3_ACCESS_KEY_ID":           "s3AccessKeyId",
	"OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME": "templateFilesBucket",
	"OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET":         "projectBlobsBucket",
	"OVERLEAF_HISTORY_BLOBS_BUCKET":                 "globalBlobsBucket",
	"BUCKET_NAME":                                   "docstoreArchiveBucket",
}

func parseStorageEnvFile(content string) Obj {
	out := Obj{}
	for _, m := range storageParseRe.FindAllStringSubmatch(content, -1) {
		if k, ok := storageEnvMap[m[1]]; ok {
			out = append(out, KV{k, m[2]})
		}
	}
	return out
}

// storageEnvExists — the managed fragment is present (the GET envManaged
// flag mirrors Node's Boolean(readStorageEnv())).
func storageEnvExists() bool {
	_, err := os.Stat(storageEnvDefaultPath())
	return err == nil
}

// storageEnvSection — parse the managed fragment into a section-ish object.
func storageEnvSection() Obj {
	content, err := os.ReadFile(storageEnvDefaultPath())
	if err != nil {
		return nil
	}
	return parseStorageEnvFile(string(content))
}

// writeStorageEnv — atomic write (temp+rename) of the managed fragment.
func writeStorageEnv(section Obj) error {
	target := storageEnvDefaultPath()
	st, err := os.Stat(target)
	if err == nil {
		_ = st
	}
	if err := os.MkdirAll(dirOf(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(renderStorageEnvFile(section)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// removeStorageEnv — unlink the managed fragment (best effort).
func removeStorageEnv() {
	_ = os.Remove(storageEnvDefaultPath())
}

func dirOf(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "."
	}
	return p[:i]
}
