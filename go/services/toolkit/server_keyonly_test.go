package toolkit

import (
	"context"
	"crypto"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clog "github.com/charmbracelet/log"
	xed25519 "golang.org/x/crypto/ed25519"
	xssh "golang.org/x/crypto/ssh"
)

func akSigner(pub xssh.PublicKey, priv crypto.PrivateKey) xssh.Signer {
	s, err := xssh.NewSignerFromKey(priv)
	if err != nil {
		panic(err)
	}
	return s
}

// TestServeKeyOnlyAuth pins the owner's key-over-passwords deployment:
// key-only Serve() must (1) accept a matching public key, (2) DENY password
// logins, and (3) fail-closed (refuse to serve) when neither keys nor a
// password are available — the state that would otherwise silently accept
// any password.
func TestServeKeyOnlyAuth(t *testing.T) {
	dir := t.TempDir()

	// a real ed25519 keypair, written ssh-keygen style
	pub, priv, err := xed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	authPub, aerr := xssh.NewPublicKey(pub)
	if aerr != nil {
		t.Fatal(aerr)
	}
	der := base64.StdEncoding.EncodeToString(authPub.Marshal())
	authLine := authPub.Type() + " " + der + " bootstrap@test\n"
	keys := filepath.Join(dir, "ssh")
	if err := os.MkdirAll(keys, 0o700); err != nil {
		t.Fatal(err)
	}
	ak := filepath.Join(keys, "authorized_keys")
	if err := os.WriteFile(ak, []byte(authLine), 0o600); err != nil {
		t.Fatal(err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	tk := &Toolkit{
		Store:        nil,
		DataDir:      dir,
		Project:      "ollitex",
		DockerSocket: dir + "/no-such-sock",
		Ver:          "test",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- tk.Serve(ctx, ServerOpts{Listen: addr, User: "ollitex", KeyFile: ak, Log: clog.New(io.Discard)})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, derr := net.Dial("tcp", addr)
		if derr == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not listening: %v", derr)
		}
		select {
		case serr := <-errCh:
			t.Fatalf("serve exited early: %v", serr)
		case <-time.After(50 * time.Millisecond):
		}
	}

	// (1) key auth ACCEPTED (openssh-format material; same path the operator
	// uses with the auto-generated pair)
	akKey, kerr2 := func() (xssh.PublicKey, error) {
		k, _, _, _, e := xssh.ParseAuthorizedKey([]byte(authLine))
		return k, e
	}()
	if kerr2 != nil {
		t.Fatal(kerr2)
	}
	cfg := &xssh.ClientConfig{
		User:            "ollitex",
		Auth:            []xssh.AuthMethod{xssh.PublicKeys(akSigner(akKey, priv))},
		HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	c, cerr := xssh.Dial("tcp", addr, cfg)
	if cerr != nil {
		t.Fatalf("key auth was DENIED: %v", cerr)
	}
	c.Close()

	// (2) password auth DENIED (no password was configured)
	pcfg := &xssh.ClientConfig{
		User:            "ollitex",
		Auth:            []xssh.AuthMethod{xssh.Password("whatever")},
		HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	if pc, perr := xssh.Dial("tcp", addr, pcfg); perr == nil {
		pc.Close()
		t.Fatalf("password auth was ACCEPTED in a key-only deployment — must be denied")
	}
}

// TestServeNoAuthFailsClosed pins the fail-closed gate: no keys + no
// password ⇒ Serve must refuse rather than fall through to wish's
// permissive default (which would accept ANY password).
func TestServeNoAuthFailsClosed(t *testing.T) {
	dir := t.TempDir()
	tk := &Toolkit{Store: nil, DataDir: dir, Project: "ollitex", DockerSocket: dir + "/s", Ver: "test"}
	err := tk.Serve(context.Background(), ServerOpts{
		Listen:  "127.0.0.1:0",
		User:    "ollitex",
		KeyFile: filepath.Join(dir, "nope", "authorized_keys"), // absent
		Log:     clog.New(io.Discard),
	})
	if err == nil {
		t.Fatalf("Serve must REFUSE when no auth method is active (fail-closed); got a started server")
	}
	if !strings.Contains(err.Error(), "no auth method") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = fmt.Sprintf("%v", err)
}
