// Command cronmail is the Go replacement for the Node email-notification
// dispatcher cron:
//
//	server-ce/cron/notification-email-dispatch.sh
//	  → services/web/scripts/process_notifications.mjs
//	    → modules/notifications/app/src/ProcessNotifications.mjs
//
// One pass per invocation (cron-friendly): claim due `emailNotifications`
// docs, send the pinned cron email types, delete/mark-failed — the exact
// node claim-protocol semantics (stale reclaim, exponential backoff,
// dead-letter, dry-run release).
//
// Boot parity with the node script: env OVERLEAF_MONGO_URL (Settings.mongo
// url), the same PROCESS_NOTIFICATIONS_* / OVERLEAF_NOTIFICATIONS_* knobs,
// and the same SMTP settings the rest of the Go web uses (OVERLEAF_EMAIL_*
// via core.Mail). Exit 0 on a clean pass (including an empty queue), 1 on
// any hard failure — matching the node script's try/main/catch shape.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"ollitex/go/mongoh"
	"ollitex/go/services/cronmail"
	"ollitex/go/services/web/core"
)

func main() {
	log.SetFlags(0)

	mongoURI := envOr("OVERLEAF_MONGO_URL", "mongodb://127.0.0.1:27017/sharelatex")
	db := mongoh.DBFromURI(mongoURI, "sharelatex") // node: client.db() on the same URI
	log.Printf("cronmail: connecting to mongo db=%q", db)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongoh.Connect(ctx, mongoh.Options{URI: mongoURI, DB: db})
	if err != nil {
		log.Printf("cronmail: fatal: cannot connect to mongo: %v", err)
		os.Exit(1)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	st := cronmail.NewMongoStore(client, db, "users")
	mail := core.NewMail() // OVERLEAF_EMAIL_SMTP_* / MAIL_* via the Go web seam
	s := cronmail.NewSettingsFromEnv()
	cfg := cronmail.NewConfigFromEnv()

	log.Printf("cronmail: %s", "Processing notifications...")
	start := time.Now()
	stats, err := cronmail.Run(ctx, st, mail, s, cfg)
	duration := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("cronmail: error processing notifications: %v", err)
		os.Exit(1)
	}
	if cfg.DryRun {
		log.Printf("cronmail: dry-run complete: %+v (%s)", stats, duration)
		return
	}
	log.Printf("cronmail: notifications processed successfully: found=%d ready=%d sent=%d (%s)",
		stats.NotificationsFound, stats.NotificationsReady, stats.EmailsSent, duration)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
