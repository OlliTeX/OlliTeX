package toolkit

// Actions — the admin actions for the TUI/CLI (owner queue 2026-10-07):
//
//  1. nginx TLS import      — copy cert+key into the host data dir, point
//      the TLS_*_PATH store keys at them, restart nginx once.
//
//  2. LanguageTool n-gram   — download the official dated archives
//      (https://languagetool.org/download/ngram-data/), stable rename
//      (ngrams-<lang>.zip), extract into <dataDir>/ngrams/<lang>.
//
// Both are host-side operations on the mounted data dir (the containers
// mount it read-only) — no exec into the stack is required, which keeps
// the host contract (docker socket + one folder) intact.

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// NgramModel is one downloadable n-gram set (official or untested tier).
type NgramModel struct {
	Lang     string
	Archive  string
	Untested bool // true → served under /untested/ (ngram-<lang>-<date>.zip)
}

// NgramModels — OFFICIAL tier (owner script, 2026-09-13) + the owner's listed
// UNTESTED tier (2026-10-06 feed).
var NgramModels = []NgramModel{
	{Lang: "en", Archive: "ngrams-en-20150817.zip"},
	{Lang: "de", Archive: "ngrams-de-20150819.zip"},
	{Lang: "es", Archive: "ngrams-es-20150915.zip"},
	{Lang: "fr", Archive: "ngrams-fr-20150913.zip"},
	{Lang: "nl", Archive: "ngrams-nl-20181229.zip"},
	// untested tier (owner list 2026-10-06)
	{Lang: "he", Archive: "ngram-he-20150916.zip", Untested: true},
	{Lang: "it", Archive: "ngram-it-20150915.zip", Untested: true},
	{Lang: "ru", Archive: "ngram-ru-20150914.zip", Untested: true},
	{Lang: "zh", Archive: "ngram-zh-20150916.zip", Untested: true},
}

// NgramArchives (compat): lang -> dated archive name (both tiers).
var NgramArchives = func() map[string]string {
	m := map[string]string{}
	for _, n := range NgramModels {
		m[n.Lang] = n.Archive
	}
	return m
}()

func ngramModel(lang string) (NgramModel, bool) {
	for i := range NgramModels {
		if NgramModels[i].Lang == lang {
			return NgramModels[i], true
		}
	}
	return NgramModel{}, false
}

// NgramBaseURL is injectable for tests (offline httptest servers).
var NgramBaseURL = "https://languagetool.org/download/ngram-data"

// NgramCodes returns the supported languages in stable order.
func NgramCodes() []string {
	ks := make([]string, 0, len(NgramArchives))
	for k := range NgramArchives {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// Ngram actions (stable strings for the TUI/CLI + tests).
const (
	NgramActionDownloaded   = "downloaded"
	NgramActionAlreadyOwned = "already-present"
	NgramActionSkipped      = "skipped-unsupported"
)

// NgramStatus is one language's outcome.
type NgramStatus struct {
	Language string
	Action   string // NgramAction* constants
	Path     string
	Size     int64
	archive  string // internal: the dated archive name
	untested bool   // internal: /untested/ tier
}

// NgramPlan is the pure, network-free decision for the selected languages:
// for each it computes the action + target paths (idempotent: an existing
// <dir>/<lang> + stable archive is already-present). The TUI/CLI shows this
// plan before anything is fetched.
func NgramPlan(dataDir string, langs []string) ([]NgramStatus, error) {
	nlDir := filepath.Join(dataDir, "ngrams")
	out := []NgramStatus{}
	seen := map[string]bool{}
	for _, raw := range langs {
		lang := strings.TrimSpace(strings.ToLower(raw))
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		model, ok := ngramModel(lang)
		if !ok {
			out = append(out, NgramStatus{Language: lang, Action: NgramActionSkipped})
			continue
		}
		// stable name ON DISK: official = ngrams-<lang>.zip (owner's wget -O
		// convention + compose_cep layout); untested keeps its dated name.
		stableName := model.Archive
		if !model.Untested {
			stableName = fmt.Sprintf("ngrams-%s.zip", lang)
		}
		stablePath := filepath.Join(nlDir, stableName)
		extractDir := filepath.Join(nlDir, lang)
		d1, _ := isDir(extractDir)
		if d1 && fileExists(stablePath) {
			out = append(out, NgramStatus{
				Language: lang, Action: NgramActionAlreadyOwned,
				Path: extractDir, Size: fileSize(stablePath),
			})
			continue
		}
		out = append(out, NgramStatus{
			Language: lang, Action: "needs-download",
			Path:     extractDir,
			archive:  model.Archive,
			untested: model.Untested,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	return out, nil
}

// NgramDownload executes the plan for every "needs-download" language and
// re-derives the outcome. Idempotent. Returns one status per language.
func NgramDownload(ctx context.Context, dataDir string, langs []string) ([]NgramStatus, error) {
	if _, err := exec.LookPath("unzip"); err != nil {
		return nil, fmt.Errorf("unzip is required (apk add unzip in the toolkit image)")
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "ngrams"), 0o755); err != nil {
		return nil, err
	}
	plan, err := NgramPlan(dataDir, langs)
	if err != nil {
		return nil, err
	}
	out := []NgramStatus{}
	for _, st := range plan {
		if st.Action != "needs-download" {
			out = append(out, st)
			continue
		}
		nlDir := filepath.Join(dataDir, "ngrams")
		stableName := st.archive
		if !st.untested {
			stableName = fmt.Sprintf("ngrams-%s.zip", st.Language)
		}
		stablePath := filepath.Join(nlDir, stableName)
		extractDir := filepath.Join(nlDir, st.Language)
		url := NgramBaseURL
		if st.untested {
			url += "/untested/"
		}
		url += "/" + st.archive
		tmp := stablePath + ".part"
		if derr := downloadFile(ctx, url, tmp); derr != nil {
			_ = os.Remove(tmp)
			out = append(out, NgramStatus{Language: st.Language, Action: "download-failed", Path: url})
			continue
		}
		if _, ierr := isDir(extractDir); ierr != nil {
			cmd := exec.Command("unzip", "-q", "-o", tmp, "-d", extractDir)
			if _, uerr := cmd.CombinedOutput(); uerr != nil {
				_ = os.Remove(tmp)
				out = append(out, NgramStatus{Language: st.Language, Action: "unzip-failed", Path: url})
				continue
			}
		}
		if err := os.Rename(tmp, stablePath); err != nil {
			_ = os.Remove(tmp)
			out = append(out, NgramStatus{Language: st.Language, Action: "rename-failed", Path: stablePath})
			continue
		}
		out = append(out, NgramStatus{
			Language: st.Language, Action: NgramActionDownloaded,
			Path: extractDir, Size: fileSize(stablePath),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	return out, nil
}

// downloadFile streams a URL into path (no full-buffer in RAM for the
// multi-GB archives).
func downloadFile(ctx context.Context, rawURL, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Close()
}

var defaultHTTPClient = &http.Client{}

func httpClient() *http.Client { return defaultHTTPClient }

func isDir(p string) (bool, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// ---- TLS import ------------------------------------------------------------

// TLSImport copies a certificate + private key (owner files on the host)
// into the stack's data dir and returns the two host paths the nginx
// service must mount (the caller stores them in TLS_CERTIFICATE_PATH /
// TLS_PRIVATE_KEY_PATH and restarts nginx).
func TLSImport(dataDir, certSrc, keySrc string) (certHost, keyHost string, err error) {
	certSrc = strings.TrimSpace(certSrc)
	keySrc = strings.TrimSpace(keySrc)
	if certSrc == "" || keySrc == "" {
		return "", "", fmt.Errorf("both the certificate and the private key path are required")
	}
	for _, p := range []string{certSrc, keySrc} {
		fi, serr := os.Stat(p)
		if serr != nil {
			return "", "", fmt.Errorf("cannot read %s: %w", p, serr)
		}
		if fi.IsDir() {
			return "", "", fmt.Errorf("%s is a directory — give the file", p)
		}
	}
	dst := filepath.Join(dataDir, "nginx", "tls")
	if merr := os.MkdirAll(dst, 0o700); merr != nil {
		return "", "", merr
	}
	certDst := filepath.Join(dst, "nginx_certificate.pem")
	keyDst := filepath.Join(dst, "nginx_key.pem")
	if cerr := copyFile(certSrc, certDst, 0o640); cerr != nil {
		return "", "", cerr
	}
	if kerr := copyFile(keySrc, keyDst, 0o600); kerr != nil {
		return "", "", kerr
	}
	return certDst, keyDst, nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

// ImportCert validates the (key, cert) pair — both parse AND the key's
// public key EQUALS the certificate's public key (cross-signing check) —
// BEFORE writing anything. On success it copies them into
// <dstBase-dir>/{k,c}.pem (key 0600) and returns the CN/subject snippet.
func ImportCert(dstKey, dstCert, srcKey, srcCert string) (string, error) {
	keyPEM, err := os.ReadFile(srcKey)
	if err != nil {
		return "", fmt.Errorf("key: %w", err)
	}
	certPEM, err := os.ReadFile(srcCert)
	if err != nil {
		return "", fmt.Errorf("cert: %w", err)
	}
	if !strings.Contains(string(keyPEM), "PRIVATE KEY") {
		return "", fmt.Errorf("key file does not look like a private key (%s)", srcKey)
	}
	keyDER, _ := pem.Decode(keyPEM)
	if keyDER == nil {
		return "", fmt.Errorf("key: no PEM block found")
	}
	certDER, _ := pem.Decode(certPEM)
	if certDER == nil || certDER.Type != "CERTIFICATE" {
		return "", fmt.Errorf("cert: no CERTIFICATE PEM block found")
	}
	x509cert, err := x509.ParseCertificate(certDER.Bytes)
	if err != nil {
		return "", fmt.Errorf("cert: %w", err)
	}
	pub, ok := parseKeyPub(keyDER)
	if !ok {
		return "", fmt.Errorf("key: unsupported private key format (%s)", srcKey)
	}
	if !pubsMatch(pub, x509cert.PublicKey) {
		return "", fmt.Errorf("key/cert MISMATCH: the private key does not hold the certificate's public key (pair rejected, nothing copied)")
	}
	if err := os.MkdirAll(filepath.Dir(dstKey), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dstCert), 0o700); err != nil {
		return "", err
	}
	if err := copyFile(srcKey, dstKey, 0o600); err != nil {
		return "", err
	}
	if err := copyFile(srcCert, dstCert, 0o640); err != nil {
		return "", err
	}
	return "subject=" + x509cert.Subject.CommonName + " not-after=" + x509cert.NotAfter.Format("2006-01-02"), nil
}

// pubsMatch compares two public keys (type-aware, constant-time-friendly).
func pubsMatch(a, b crypto.PublicKey) bool {
	are, okA := a.(*rsa.PublicKey)
	bre, okB := b.(*rsa.PublicKey)
	if okA && okB {
		return are.Equal(bre)
	}
	ae, okA := a.(*ecdsa.PublicKey)
	bee, okB := b.(*ecdsa.PublicKey)
	if okA && okB {
		return ae.Equal(bee)
	}
	return false
}

// parseKeyPub extracts the public key from the private key's PEM block.
func parseKeyPub(b *pem.Block) (crypto.PublicKey, bool) {
	if k, err := x509.ParsePKCS8PrivateKey(b.Bytes); err == nil {
		switch k := k.(type) {
		case *rsa.PrivateKey:
			return &k.PublicKey, true
		case *ecdsa.PrivateKey:
			return &k.PublicKey, true
		}
		return nil, false
	}
	if k, err := x509.ParsePKCS1PrivateKey(b.Bytes); err == nil {
		return &k.PublicKey, true
	}
	if k, err := x509.ParseECPrivateKey(b.Bytes); err == nil {
		return &k.PublicKey, true
	}
	return nil, false
}

// NgramSizeFor is the human size of a stable archive (for the UI display).
func NgramSizeFor(dataDir, lang string) int64 {
	return fileSize(filepath.Join(dataDir, "ngrams", fmt.Sprintf("ngrams-%s.zip", lang)))
}

// HumanBytes renders bytes for the UI.
func HumanBytes(n int64) string {
	if n <= 0 {
		return "-"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "kMGTPE"[exp])
}
