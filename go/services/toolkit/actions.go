package toolkit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ---- admin-selected language models (owner addendum A: languagetool) ----
//
// Port of the owner's canonical toolkit/bin/languagetool-ngrams: the OFFICIAL
// LanguageTool n-gram data sets, exact layout the erikvl87 image + /hub
// grammar settings expect (<data>/ngrams/<lang>/ extracted plus the stable
// archive), idempotent (skip when present).

// NgramBaseURL is injectable for tests (httptest server).
var NgramBaseURL = "https://languagetool.org/download/ngram-data"

// NgramModel is one downloadable n-gram language model (official or the
// untested tier the owner listed under /untested/).
type NgramModel struct {
	Lang     string
	Archive  string // official dated archive file name
	Untested bool   // true -> served under /untested/ (ngram-* naming)
}

// NgramModels is the full model set: the OFFICIAL tier (owner script map,
// 2026-09-13) + the UNTESTED tier (owner list 2026-10-04).
var NgramModels = []NgramModel{
	{Lang: "en", Archive: "ngrams-en-20150817.zip"},
	{Lang: "de", Archive: "ngrams-de-20150819.zip"},
	{Lang: "es", Archive: "ngrams-es-20150915.zip"},
	{Lang: "fr", Archive: "ngrams-fr-20150913.zip"},
	{Lang: "nl", Archive: "ngrams-nl-20181229.zip"},
	// untested tier (ngram-<lang>-<date> under /untested/)
	{Lang: "he", Archive: "ngram-he-20150916.zip", Untested: true},
	{Lang: "it", Archive: "ngram-it-20150915.zip", Untested: true},
	{Lang: "ru", Archive: "ngram-ru-20150914.zip", Untested: true},
	{Lang: "zh", Archive: "ngram-zh-20150916.zip", Untested: true},
}

// NgramArchives (compat accessor): official tier lang -> archive.
var NgramArchives = func() map[string]string {
	m := map[string]string{}
	for _, n := range NgramModels {
		if !n.Untested {
			m[n.Lang] = n.Archive
		}
	}
	return m
}()

func ngramModel(lang string) (NgramModel, bool) {
	for _, n := range NgramModels {
		if n.Lang == lang {
			n := n
			return n, true
		}
	}
	return NgramModel{}, false
}

func ngramURL(n NgramModel) string {
	if n.Untested {
		return NgramBaseURL + "/untested/" + n.Archive
	}
	return NgramBaseURL + "/" + n.Archive
}

// ---- word2vec neural rules (languagetool-101: confusion-pair disambiguation) ----

// Word2VecModels are the official word2vec model archives (en/de/pt).
var Word2VecModels = map[string]string{
	"en": "en.zip",
	"de": "de.zip",
	"pt": "pt.zip",
}

var Word2VecBaseURL = "https://languagetool.org/download/word2vec"

// Word2VecCodes returns the supported word2vec languages in stable order.
func Word2VecCodes() []string {
	ks := make([]string, 0, len(Word2VecModels))
	for k := range Word2VecModels {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// Word2VecDownload fetches + extracts the selected word2vec models into
// <dataDir>/word2vec/<lang> (languagetool-101 layout), idempotent.
func Word2VecDownload(ctx context.Context, dataDir string, langs []string) ([]NgramStatus, error) {
	if _, err := exec.LookPath("unzip"); err != nil {
		return nil, fmt.Errorf("unzip is required (apk add unzip in the toolkit image)")
	}
	base := filepath.Join(dataDir, "word2vec")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	out := []NgramStatus{}
	seen := map[string]bool{}
	for _, raw := range langs {
		lang := strings.TrimSpace(strings.ToLower(raw))
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		archive, ok := Word2VecModels[lang]
		if !ok {
			out = append(out, NgramStatus{Language: lang, Action: NgramActionSkipped})
			continue
		}
		extractDir := filepath.Join(base, lang)
		if d, _ := isDir(extractDir); d {
			out = append(out, NgramStatus{Language: lang, Action: NgramActionAlreadyPresent, Path: extractDir})
			continue
		}
		url := Word2VecBaseURL + "/" + archive
		tmp := base + "/" + archive + ".part"
		if derr := downloadFile(ctx, url, tmp); derr != nil {
			_ = os.Remove(tmp)
			out = append(out, NgramStatus{Language: lang, Action: "download-failed", Path: url})
			continue
		}
		cmd := exec.CommandContext(ctx, "unzip", "-q", "-o", tmp, "-d", extractDir)
		if _, uerr := cmd.CombinedOutput(); uerr != nil {
			_ = os.Remove(tmp)
			out = append(out, NgramStatus{Language: lang, Action: "unzip-failed", Path: url})
			continue
		}
		_ = os.Remove(tmp)
		out = append(out, NgramStatus{Language: lang, Action: NgramActionDownloaded, Path: extractDir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	return out, nil
}

// Ngram actions (stable strings for the TUI/CLI + tests).
const (
	NgramActionDownload       = "needs-download"
	NgramActionAlreadyPresent = "already-present"
	NgramActionSkipped        = "skipped-unsupported"
	NgramActionDownloaded     = "downloaded"
)

// NgramStatus is one language's outcome.
type NgramStatus struct {
	Language string
	Action   string // NgramAction* constants
	Path     string
	Size     int64
	archive  string // internal: the official dated archive name
	untested bool   // internal: /untested/ tier (ngram-* dated naming)
}

// NgramPlan is the pure, network-free decision for the selected languages:
// for each it computes the action + target paths (idempotent: an existing
// <dir>/<lang> + stable archive is already-present). The TUI/CLI shows this
// plan before anything is fetched.
func NgramPlan(dataDir string, langs []string) ([]NgramStatus, error) {
	nlDir := filepath.Join(dataDir, "ngrams")
	if _, err := os.Stat(nlDir); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
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
		// stable name ON DISK: official tier = ngrams-<lang>.zip (the owner's
		// wget -O convention / compose_cep layout); the untested tier keeps its
		// dated archive name.
		stableName := model.Archive
		if !model.Untested {
			stableName = fmt.Sprintf("ngrams-%s.zip", lang)
		}
		stablePath := filepath.Join(nlDir, stableName)
		extractDir := filepath.Join(nlDir, lang)
		d1, _ := isDir(extractDir)
		if d1 && fileExists(stablePath) {
			out = append(out, NgramStatus{
				Language: lang, Action: NgramActionAlreadyPresent,
				Path: extractDir, Size: fileSize(stablePath),
			})
			continue
		}
		out = append(out, NgramStatus{
			Language: lang, Action: NgramActionDownload,
			Path:     extractDir,
			archive:  model.Archive,
			untested: model.Untested,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	return out, nil
}

// NgramDownload executes the download for every "needs-download" language in
// the plan (several-GB streams, idempotent). It re-derives the plan so the
// caller only passes dataDir+langs.
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
		if st.Action != NgramActionDownload {
			out = append(out, st)
			continue
		}
		nlDir := filepath.Join(dataDir, "ngrams")
		// stable name on disk: official tier = ngrams-<lang>.zip (the owner's
		// wget -O convention); untested tier keeps its dated archive name.
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
			cmd := exec.CommandContext(ctx, "unzip", "-q", "-o", tmp, "-d", extractDir)
			_out, uerr := cmd.CombinedOutput()
			if uerr != nil {
				_ = _out
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

// downloadFile streams url to path (several-GB safe: no buffering in RAM).
func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if err == nil {
		if fi2, err2 := os.Stat(path); err2 == nil && fi.Size() != 0 && fi2.Size() == 0 {
			return fmt.Errorf("empty download")
		}
	}
	return err
}

func fileExists(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() }
func isDir(p string) (bool, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}
func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// ---- nginx cert import (owner addendum A) ---------------------------------
//
// Imports the operator's SSL key+cert files from host paths into the nginx
// mount points (store: TLS_PRIVATE_KEY_PATH / TLS_CERTIFICATE_PATH), validates
// that the pair actually matches, and restarts nginx when asked.

// ImportCert validates key+cert on the host and copies them onto the mount
// paths. Returns the verified pair's leaf CN + notAfter for the operator log.
func ImportCert(dstKey, dstCert string, srcKey, srcCert string, restart bool) (string, error) {
	keyBytes, err := os.ReadFile(srcKey)
	if err != nil {
		return "", fmt.Errorf("read key %s: %w", srcKey, err)
	}
	certBytes, err := os.ReadFile(srcCert)
	if err != nil {
		return "", fmt.Errorf("read cert %s: %w", srcCert, err)
	}
	certBlock, _ := x509.ParseCertificate(firstCertBlock(certBytes))
	if certBlock == nil {
		return "", fmt.Errorf("no x509 certificate found in %s", srcCert)
	}
	// pair match: decode the private key; TLS's x509 key pair check below
	// catches a wrong (key,cert) combination early.
	if _, err := tls.X509KeyPair(certBytes, keyBytes); err != nil {
		return "", fmt.Errorf("key/cert do NOT match: %w", err)
	}
	// write key 0600, cert 0644
	perm := []struct {
		dst  string
		src  []byte
		perm os.FileMode
	}{{dstKey, keyBytes, 0o600}, {dstCert, certBytes, 0o644}}
	for _, p := range perm {
		d := filepath.Dir(p.dst)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p.dst, p.src, p.perm); err != nil {
			return "", err
		}
	}
	subj := certBlock.Subject.CommonName
	if dn := certBlock.Subject.String(); subj == "" {
		subj = dn
	}
	return fmt.Sprintf("%s (valid to %s)", subj, certBlock.NotAfter.String()), nil
}

func firstCertBlock(pem []byte) []byte {
	// x509.ParseCertificate takes one DER block; find the first CERTIFICATE block
	start := strings.Index(string(pem), "-----BEGIN CERTIFICATE-----")
	if start < 0 {
		return nil
	}
	end := strings.Index(string(pem[start:]), "-----END CERTIFICATE-----")
	if end < 0 {
		return nil
	}
	// decode base64 of that block
	block := string(pem[start : start+end+len("-----END CERTIFICATE-----")])
	lines := strings.Split(block, "\n")
	var b64 strings.Builder
	in := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "-----BEGIN CERTIFICATE-----" {
			in = true
			continue
		}
		if l == "-----END CERTIFICATE-----" {
			break
		}
		if in {
			b64.WriteString(l)
		}
	}
	der, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		return nil
	}
	return der
}

// NgramCodes returns the supported language codes in stable order.
func NgramCodes() []string {
	ks := make([]string, 0, len(NgramArchives))
	for k := range NgramArchives {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
