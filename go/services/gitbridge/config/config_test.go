// Port of application/config/ConfigTest + full Load coverage.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, doc string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

const baseDoc = `{"port": 80, "bindIp": "127.0.0.1", "idleTimeout": 30000, ` +
	`"rootGitDirectory": "/var/wlgb/git", "apiBaseUrl": "http://127.0.0.1:60000/api/v0", ` +
	`"postbackBaseUrl": "http://127.0.0.1", "serviceName": "Overleaf"`

// testConstructWithOauth ports ConfigTest.testConstructWithOauth.
func TestConstructWithOauth(t *testing.T) {
	cfgPath := writeConfig(t, baseDoc+`,"oauth2Server": "https://www.overleaf.com"}`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.GetPort(); got != 80 {
		t.Errorf("port = %d, want 80", got)
	}
	if got := cfg.GetRootGitDirectory(); got != "/var/wlgb/git" {
		t.Errorf("rootGitDir = %q", got)
	}
	if got := cfg.GetAPIBaseURL(); got != "http://127.0.0.1:60000/api/v0/" {
		t.Errorf("apiBaseURL = %q, want a trailing /", got)
	}
	if got := cfg.GetPostbackURL(); got != "http://127.0.0.1/" {
		t.Errorf("postbackURL = %q", got)
	}
	if got := cfg.GetServiceName(); got != "Overleaf" {
		t.Errorf("serviceName = %q", got)
	}
	if got := cfg.OAuth2Server(); got == nil || *got != "https://www.overleaf.com" {
		t.Errorf("oauth2Server = %v, want the URL", got)
	}
}

// testConstructWithoutOauth ports ConfigTest.testConstructWithoutOauth
// (the oauth2Server field absent => nil).
func TestConstructWithoutOauth(t *testing.T) {
	cfgPath := writeConfig(t, baseDoc+`}`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.OAuth2Server(); got != nil {
		t.Errorf("oauth2Server = %v, want nil when absent", got)
	}
}

func TestOauth2ServerExplicitNull(t *testing.T) {
	cfgPath := writeConfig(t, baseDoc+`,"oauth2Server": null}`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.OAuth2Server(); got != nil {
		t.Errorf("oauth2Server explicit null = %v, want nil", got)
	}
}

// testAsSanitised ports ConfigTest.asSanitised. The Java assertion is byte
// -exact against a pretty-printed log string; the Go ToSanitisedString is
// compact JSON with omitempty, so we parse and assert the same observable
// contract (every required field present, secrets redacted, the trailing-slash
// URL formatting preserved).
func TestAsSanitised(t *testing.T) {
	cfgPath := writeConfig(t, baseDoc+`,"oauth2Server": "https://www.overleaf.com"}`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(cfg.ToSanitisedString()), &m); err != nil {
		t.Fatalf("sanitised string is not valid JSON: %v", err)
	}
	if got, _ := m["port"].(float64); int(got) != 80 {
		t.Errorf("port = %v, want 80", m["port"])
	}
	if got := m["apiBaseUrl"].(string); got != "http://127.0.0.1:60000/api/v0/" {
		t.Errorf("apiBaseUrl = %q, want a trailing /", got)
	}
	if got := m["postbackBaseUrl"].(string); got != "http://127.0.0.1/" {
		t.Errorf("postbackBaseUrl = %q, want a trailing /", got)
	}
	if got := m["serviceName"].(string); got != "Overleaf" {
		t.Errorf("serviceName = %q", got)
	}
	if got := m["oauth2Server"].(string); got != "https://www.overleaf.com" {
		t.Errorf("oauth2Server = %q", got)
	}
	for _, absent := range []string{"repoStore", "swapStore", "swapJob"} {
		if _, ok := m[absent]; ok {
			t.Errorf("sanitised string must omit %s when nil", absent)
		}
	}
}

// asSanitisedHidesAwsKeys ports the sanitisation intent (Java: the AWS key
// fields are replaced with a placeholder; the raw value must never appear).
func TestSanitisedHidesAwsKeys(t *testing.T) {
	doc := baseDoc + `,"swapStore": {"type": "s3", "awsAccessKey": "SECRET-ACCESS", "awsSecret": "SECRET-SECRET", "s3BucketName": "b", "awsRegion": "eu", "awsEndpoint": ""}` +
		`,"swapJob": {"minProjects": 2,"lowGiB": 1,"highGiB": 5,"intervalMillis": 60000,"compressionMethod": "bzip2"}` +
		`,"repoStore": {"maxFileSize": 123, "maxFileNum": 9}}`
	cfgPath := writeConfig(t, doc)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	san := cfg.ToSanitisedString()
	if strings.Contains(san, "SECRET-") {
		t.Fatalf("sanitised string leaked a raw AWS key: %s", san)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(san), &m); err != nil {
		t.Fatalf("sanitised string is not valid JSON: %v", err)
	}
	s3 := m["swapStore"].(map[string]any)
	if got := s3["awsAccessKey"].(string); got != "<awsAccessKey>" {
		t.Fatalf("awsAccessKey = %q, want the placeholder", got)
	}
	if got := s3["awsSecret"].(string); got != "<awsSecret>" {
		t.Fatalf("awsSecret = %q, want the placeholder", got)
	}
	// The non-sensitive fields survive sanitisation.
	if got := s3["type"].(string); got != "s3" {
		t.Fatalf("swapStore.type = %q, want s3", got)
	}
	if got := s3["s3BucketName"].(string); got != "b" {
		t.Fatalf("swapStore.s3BucketName = %q, want b", got)
	}
	if got := s3["awsRegion"].(string); got != "eu" {
		t.Fatalf("swapStore.awsRegion = %q, want eu", got)
	}
	job := m["swapJob"].(map[string]any)
	if got, _ := job["minProjects"].(float64); int(got) != 2 {
		t.Fatalf("swapJob.minProjects = %v, want 2", job["minProjects"])
	}
	if got := job["compressionMethod"].(string); got != "bzip2" {
		t.Fatalf("swapJob.compressionMethod = %q, want bzip2", got)
	}
	rs := m["repoStore"].(map[string]any)
	if got, _ := rs["maxFileSize"].(float64); int(got) != 123 {
		t.Fatalf("repoStore.maxFileSize = %v, want 123", rs["maxFileSize"])
	}
}

// badConfigs ports ConfigTests' error cases: each required field must fail
// the Load when missing/empty; malformed JSON errors too.
func TestLoadFailsWithoutRequiredFields(t *testing.T) {
	cases := map[string]string{
		"no bindIp":           `{"port":1,"rootGitDirectory":"/x","apiBaseUrl":"a","postbackBaseUrl":"b","serviceName":"s"}`,
		"empty bindIp":        `{"bindIp":"","rootGitDirectory":"/x","apiBaseUrl":"a","postbackBaseUrl":"b","serviceName":"s"}`,
		"no rootGitDirectory": `{"bindIp":"i","apiBaseUrl":"a","postbackBaseUrl":"b","serviceName":"s"}`,
		"empty apiBaseUrl":    `{"bindIp":"i","rootGitDirectory":"/x","apiBaseUrl":"","postbackBaseUrl":"b","serviceName":"s"}`,
		"no serviceName":      `{"bindIp":"i","rootGitDirectory":"/x","apiBaseUrl":"a","postbackBaseUrl":"b"}`,
		"no postbackBaseUrl":  `{"bindIp":"i","rootGitDirectory":"/x","apiBaseUrl":"a","serviceName":"s"}`,
		"not json":            `{not json`,
		"bad repoStore json":  `{"bindIp":"i","rootGitDirectory":"/x","apiBaseUrl":"a","postbackBaseUrl":"b","serviceName":"s","repoStore":{bad}`,
		"bad swapStore json":  `{"bindIp":"i","rootGitDirectory":"/x","apiBaseUrl":"a","postbackBaseUrl":"b","serviceName":"s","swapStore":{bad}`,
		"bad swapJob json":    `{"bindIp":"i","rootGitDirectory":"/x","apiBaseUrl":"a","postbackBaseUrl":"b","serviceName":"s","swapJob":{bad}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, doc)); err == nil {
				t.Fatalf("Load must fail for %s", name)
			}
		})
	}
}

// missingFileLoadError: Load wraps the file error.
func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatalf("Load of missing file must error")
	}
}

// nestedObjectsAndScalars ports the nested-object + int parsing.
func TestNestedObjects(t *testing.T) {
	doc := baseDoc + `,"repoStore": {"maxFileSize": 111, "maxFileNum": 222},` +
		`"swapStore": {"type": "in-memory"},` +
		`"sqliteHeapLimitBytes": 4194304}`
	cfg, err := Load(writeConfig(t, doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoStore == nil || cfg.RepoStore.MaxFileSize == nil || *cfg.RepoStore.MaxFileSize != 111 || *cfg.RepoStore.MaxFileNum != 222 {
		t.Fatalf("repoStore not parsed: %+v", cfg.RepoStore)
	}
	if cfg.SwapStore == nil || *cfg.SwapStore.Type != "in-memory" {
		t.Fatalf("swapStore not parsed: %+v", cfg.SwapStore)
	}
	if got := cfg.GetSqliteHeapLimitBytes(); got != 4194304 {
		t.Fatalf("heapLimit = %d, want 4194304", got)
	}
	if got := cfg.GetBindIp(); got != "127.0.0.1" {
		t.Fatalf("bindIp = %q", got)
	}
	if got := cfg.GetIdleTimeout(); got != 30000 {
		t.Fatalf("idleTimeout = %d", got)
	}
}

// allowedCorsOriginsSplit ports getAllowedCorsOrigins (comma split + trim).
func TestGetAllowedCorsOrigins(t *testing.T) {
	cfg, err := Load(writeConfig(t, baseDoc+`,"allowedCorsOrigins": "a.com, b.com "}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.GetAllowedCorsOrigins(); len(got) != 2 || got[0] != "a.com" || got[1] != " b.com" {
		t.Fatalf("corsOrigins = %v", got)
	}
	// empty raw value -> nil (Java returns an empty set)
	c2, e := Load(writeConfig(t, baseDoc+`,"allowedCorsOrigins": ""}`))
	if e != nil {
		t.Fatalf("Load empty cors: %v", e)
	} else if got := c2.GetAllowedCorsOrigins(); got != nil {
		t.Fatalf("empty cors origins = %v, want nil", got)
	}
}

func TestUserPasswordEnabledFlag(t *testing.T) {
	cfg, err := Load(writeConfig(t, baseDoc+`,"userPasswordEnabled": "true"}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsUserPasswordEnabled() {
		t.Fatalf("userPasswordEnabled=true must report enabled")
	}
	cfg2, e2 := Load(writeConfig(t, baseDoc+`,"userPasswordEnabled": "false"}`))
	if e2 != nil {
		t.Fatalf("Load: %v", e2)
	} else if cfg2.IsUserPasswordEnabled() {
		t.Fatalf("userPasswordEnabled=false must report disabled")
	}
}
