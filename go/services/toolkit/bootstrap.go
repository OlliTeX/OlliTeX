package toolkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	la "ollitex/go/services/web/features/launchpad"
)

// --- first-admin bootstrap (absorbs /launchpad register_admin — owner
// decision 2026-10-05, TODO-f3b6d88d). The app's /launchpad page is
// retired: a fresh deployment creates its first admin through the toolkit
// instead of a browser visit.
//
// The validation ladder, admin-exists gate and user-document creation are
// imported directly from the launchpad package — the exact code path the
// POST /launchpad/register_admin endpoint used (Node-parity baseline
// NewUserDoc + isAdmin + emails entry), so the created document is
// identical. Only the transport changed (CLI, not POST JSON).

// ErrBootstrapEmailRegistered mirrors launchpad.ErrEmailAlreadyRegistered
// (a non-holding user already owns the email — Node: 500 throw).
var ErrBootstrapEmailRegistered = errors.New("first admin bootstrap: EmailAlreadyRegistered")

// BootstrapFirstAdmin implements the POST /launchpad/register_admin
// success path as a CLI action:
//
//  1. an admin already exists         → error (page would 403)
//  2. email invalid                   → error
//  3. password rule fails             → error with the rule text
//  4. non-holding user owns the email → ErrBootstrapEmailRegistered
//  5. create / promote                → the Node-parity user document
func (t *Toolkit) BootstrapFirstAdmin(email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("admin email is required (usage: toolkit bootstrap --email <a@b.c> --password <pw>)")
	}
	if msg := la.ValidateEmail(email); msg != "" {
		return fmt.Errorf("email not valid: %s", msg)
	}
	if msg := la.ValidatePassword(password, email); msg != "" {
		return fmt.Errorf("invalid password: %s", msg)
	}

	dsn := t.val("MONGO_URL")
	if dsn == "" {
		return fmt.Errorf("no MONGO_URL set (content DB must be running) — start the stack or set MONGO_URL in the store")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		return fmt.Errorf("mongo connect: %w", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("mongo ping: %w (is MongoDB up and reachable?)", err)
	}

	dbName := t.val("MONGO_DB")
	if dbName == "" {
		dbName = "sharelatex"
	}
	db := client.Database(dbName)

	exists, err := la.AdminExists(ctx, db)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("an admin user already exists in %s — nothing to do (the launchpad page would have answered 403)", dbName)
	}

	analyticsID := fmt.Sprintf("toolkit-%d", time.Now().UnixNano())
	cerr := la.CreateLocalAdminOrReuse(ctx, db, email, password, analyticsID)
	switch {
	case cerr == nil:
	case cerr == la.ErrEmailAlreadyRegistered:
		return fmt.Errorf("a user with email %s already exists (launchpad: EmailAlreadyRegistered — hard stop; pick another email or promote that user manually)", email)
	default:
		return fmt.Errorf("create first admin: %w", cerr)
	}
	fmt.Fprintln(os.Stderr, "first admin created — sign in with that email and password")
	return nil
}

// BootstrapFreshInstance — the full first-boot story the /launchpad page
// used to carry (owner: "we want to have that functionality in the toolkit
// instead"): config store (key + env/defaults seed, never clobbers) then
// the first admin. A fresh deployment reaches a usable instance with zero
// browser visits.
func (t *Toolkit) BootstrapFreshInstance(email, password string, out io.Writer) error {
	if err := InitStore(t, out); err != nil {
		return err
	}
	fmt.Fprintln(out, "store ready — creating the first admin user")
	return t.BootstrapFirstAdmin(email, password)
}
