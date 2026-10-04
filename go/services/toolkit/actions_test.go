package toolkit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNgramPlan_PureDecisions(t *testing.T) {
	codes := NgramCodes()
	for _, want := range []string{"en", "de", "es", "fr", "nl"} {
		found := false
		for _, c := range codes {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("NgramCodes missing %s: %v", want, codes)
		}
	}
	dir := t.TempDir()
	// fresh: en needs download, xx skipped
	plan, err := NgramPlan(dir, []string{"en", "xx", "en", "EN"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 {
		t.Fatalf("dedup+order: %+v", plan)
	}
	byLang := map[string]string{}
	for _, s := range plan {
		byLang[s.Language] = s.Action
	}
	if byLang["en"] != "needs-download" || byLang["xx"] != NgramActionSkipped {
		t.Fatalf("actions: %+v", byLang)
	}
	// idempotency: present → already-present
	nl := filepath.Join(dir, "ngrams", "de")
	if err := os.MkdirAll(nl, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ngrams", "ngrams-de.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err = NgramPlan(dir, []string{"de"})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Action != NgramActionAlreadyOwned {
		t.Fatalf("idempotency: %+v", plan[0])
	}
}

// tinyValidZip builds a one-entry zip in memory.
func tinyValidZip(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNgramDownload_Offline(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skipf("unzip required: %v", err)
	}
	// fake upstream serving the "nl" archive (smallest official set)
	zipBytes := tinyValidZip(t, "word.txt") // owner layout: contents at zip root (unzip -d <lang>/
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+NgramArchives["nl"] {
			http.Error(w, "unexpected archive "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Write(zipBytes)
	}))
	defer srv.Close()
	old := NgramBaseURL
	NgramBaseURL = srv.URL
	defer func() { NgramBaseURL = old }()

	dir := t.TempDir()
	sts, err := NgramDownload(context.Background(), dir, []string{"nl", "zz"})
	if err != nil {
		t.Fatalf("err: %v statuses: %+v", err, sts)
	}
	if len(sts) != 2 {
		t.Fatalf("two statuses: %+v", sts)
	}
	if sts[0].Action != NgramActionDownloaded {
		t.Fatalf("nl status: %+v", sts)
	}
	byLang := map[string]NgramStatus{}
	for _, s := range sts {
		byLang[s.Language] = s
	}
	got, ok := byLang["nl"]
	if !ok || got.Action != NgramActionDownloaded {
		t.Fatalf("nl not downloaded: %+v", got)
	}
	extracted := filepath.Join(dir, "ngrams", "nl", "word.txt")
	b, err := os.ReadFile(extracted)
	if err != nil || string(b) != "payload" {
		t.Fatalf("extracted file: %v %q", err, b)
	}
	// second pass: already-present (no re-download)
	sts2, err := NgramDownload(context.Background(), dir, []string{"nl"})
	if err != nil {
		t.Fatal(err)
	}
	if sts2[0].Action != NgramActionAlreadyOwned {
		t.Fatalf("second pass: %+v", sts2[0])
	}
}

func selfSignedPair(t *testing.T) (keyPEM, certPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tuitool.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
	return
}

func TestImportCert_ValidPair(t *testing.T) {
	keyPEM, certPEM := selfSignedPair(t)
	d := t.TempDir()
	srcKey := d + "/k.pem"
	srcCert := d + "/c.pem"
	if err := os.WriteFile(srcKey, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcCert, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	dstBase := t.TempDir()
	info, err := ImportCert(dstBase+"/k.pem", dstBase+"/c.pem", srcKey, srcCert)
	if err != nil {
		t.Fatalf("valid pair: %v", err)
	}
	if info == "" {
		t.Fatal("expected subject info")
	}
	fi, err := os.Stat(dstBase + "/k.pem")
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key perms: %v %v", err, fi)
	}
}

func TestImportCert_MismatchedPairRejected(t *testing.T) {
	keyPEM, _ := selfSignedPair(t)
	_, otherCert := selfSignedPair(t)
	d := t.TempDir()
	kp := d + "/k.pem"
	cp := d + "/c.pem"
	if err := os.WriteFile(kp, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, otherCert, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportCert(d+"/o/k", d+"/o/c", kp, cp); err == nil {
		t.Fatal("mismatched key/cert must be rejected before any copy")
	}
	// and nothing copied on rejection
	if _, err := os.Stat(d + "/o/k"); !os.IsNotExist(err) {
		t.Fatal("no key copy expected on rejection")
	}
}

func TestNgramPlan_RealOwnerDataDir(t *testing.T) {
	// The owner's live production dir (their own wget -O layout, dated
	// archives renamed to the stable ngrams-<lang>.zip names). If this
	// machine runs the test, the plan must short-circuit ALL five official
	// languages to already-present — no network, no re-download.
	const ownerDir = "/data_1/docker/compose_cep/languagetool"
	if _, err := os.Stat(ownerDir + "/ngrams/en"); err != nil {
		t.Skipf("owner ngrams dir not present here: %v", err)
	}
	plan, err := NgramPlan(ownerDir, []string{"en", "de", "es", "fr", "nl"})
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range plan {
		if st.Action != NgramActionAlreadyOwned {
			t.Fatalf("%s: %s (expected already-present on the owner dir)", st.Language, st.Action)
			_ = st.Size
		}
	}
	if len(plan) != 5 {
		t.Fatalf("five languages: %+v", plan)
	}
}
