package launchpad

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Toolkit-facing API — exported wrappers around the (unexported) primitives
// so the standalone toolkit CLI can absorb the launchpad's first-admin
// function (owner decision 2026-10-05: "/launchpad functionality in the
// toolkit instead, then retire the page").
//
// Semantics are unchanged (pinned launchpad contract + Node-parity user
// doc): only visibility moves.

// ErrEmailAlreadyRegistered — the users.go sentinel, exported for external
// consumers (toolkit bootstrap).
var ErrEmailAlreadyRegistered = errEmailAlreadyRegistered

// AdminExists — true when any user has isAdmin (adminExists).
func AdminExists(ctx context.Context, db *mongo.Database) (bool, error) {
	return adminExists(ctx, db)
}

// CreateLocalAdminOrReuse — the register_admin creation path (create +
// holding-account reuse + EmailAlreadyRegistered hard stop).
func CreateLocalAdminOrReuse(ctx context.Context, db *mongo.Database, email, password, analyticsID string) error {
	return createLocalAdminOrReuse(ctx, db, email, password, analyticsID)
}

// ValidateEmail / ValidatePassword — the launchpad form ladder (exported so
// the toolkit CLI reports the same rule text).
func ValidateEmail(email string) string        { return validateEmail(email) }
func ValidatePassword(pw, email string) string { return validatePassword(pw, email) }
