package cronmail

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// Config — node env-parity (services/web/modules/notifications/app/src/
// ProcessNotifications.mjs top-level constants).
type Config struct {
	BatchSize   int           // PROCESS_NOTIFICATIONS_BATCH_SIZE || 100
	MaxAttempts int           // OVERLEAF_NOTIFICATIONS_MAX_ATTEMPTS || 3
	BackoffBase time.Duration // OVERLEAF_NOTIFICATION_SILENCE_PERIOD_MS || 2h (doubling per attempt)
	DryRun      bool          // OVERLEAF_NOTIFICATIONS_DRY_RUN === 'true'
}

// NewConfigFromEnv mirrors the node env block (node `Number(x) || fallback`).
func NewConfigFromEnv() Config {
	return Config{
		BatchSize:   intEnv("PROCESS_NOTIFICATIONS_BATCH_SIZE", 100),
		MaxAttempts: intEnv("OVERLEAF_NOTIFICATIONS_MAX_ATTEMPTS", 3),
		BackoffBase: durEnvMS("OVERLEAF_NOTIFICATION_SILENCE_PERIOD_MS", 2*time.Hour),
		DryRun:      Env("OVERLEAF_NOTIFICATIONS_DRY_RUN") == "true",
	}
}

// Mailer — the send seam. `Send` = node `EmailSender.sendEmail(options)`
// (to/from/subject/html/text; this stack's cron opts never carry `from`,
// `replyTo`, `cc`, `category` or `sendingUser_id` — the node rate-limiter
// check is therefore always the no-limit branch, which is what this
// interface assumes — documented parity, not an omission).
type Mailer interface {
	Send(to, subject, text, html string) error
}

// Stats — node `processNotifications` return (dry-run counters included).
type Stats struct {
	NotificationsFound  int `json:"notificationsFound"`
	NotificationsReady  int `json:"notificationsReady"`
	EmailsSent          int `json:"emailsSent"`
	DryRunProcessed     int `json:"dryRunProcessed,omitempty"`
	DryRunWouldHaveSent int `json:"dryRunWouldHaveSent,omitempty"`
}

// buildEmailTask — node `_buildEmailTask` (+ the recipient/project fallbacks):
//
//	emailType = notification.emailType || notification.type   (required)
//	opts      = notification.opts || notification.options || notification.data || {}
//	recap     = recipient_id → opts.toUserId when unset
//	proj      = project_id → opts.projectId when unset
type rawTask struct {
	emailType string
	raw       map[string]any // native-typed bag (bools survive)
	strings   Opts           // string view for send/render inputs
}

func buildEmailTask(d *Doc) (*rawTask, error) {
	emailType := d.EmailType
	if emailType == "" {
		emailType = d.Type
	}
	if emailType == "" {
		return nil, fmt.Errorf("scheduled email notification is missing emailType")
	}

	raw := d.Raw
	if raw == nil {
		raw = map[string]any{}
	}

	if d.RecipientID != nil {
		if _, hasTo := raw["to"]; !hasTo {
			if _, hasTU := raw["toUserId"]; !hasTU {
				raw["toUserId"] = d.RecipientID.Hex()
			}
		}
	}
	if d.ProjectID != nil {
		if _, has := raw["projectId"]; !has {
			raw["projectId"] = d.ProjectID.Hex()
		}
	}
	return &rawTask{emailType: emailType, raw: raw, strings: toStr(raw)}, nil
}

func toStr(m map[string]any) Opts {
	o := Opts{}
	for k, v := range m {
		if s, ok := v.(string); ok {
			o[k] = s
		}
	}
	return o
}

// Run — node `processNotifications`, 1:1 claim loop.
//
//	claim → build task → resolve recipient (toUserId → user email) →
//	dry-run? count : send + delete
//	error  → attempts += 1; dead-letter at MAX_ATTEMPTS else exponential
//	backoff (base * 2^(attempts-1)); dry-run claims released in one batch.
func Run(ctx context.Context, st Store, mail Mailer, s Settings, cfg Config) (Stats, error) {
	var stats Stats
	var dryClaimed []bson.Raw

	for stats.NotificationsFound < cfg.BatchSize {
		d, err := st.ClaimNextDue(ctx, time.Now())
		if err != nil {
			return stats, err
		}
		if d == nil {
			break
		}
		stats.NotificationsFound++

		task, err := buildEmailTask(d)
		if err != nil {
			if rerr := markFailed(ctx, st, d, cfg, err); rerr != nil {
				return stats, rerr
			}
			continue
		}

		// recipient resolution (node: opts.toUserId → getUser({email:1}))
		if _, hasTo := task.raw["to"]; !hasTo {
			if tu, hasTU := task.raw["toUserId"]; hasTU {
				tuStr, _ := tu.(string)
				email, uerr := st.GetUserEmail(ctx, tu)
				if uerr != nil {
					if rerr := markFailed(ctx, st, d, cfg, uerr); rerr != nil {
						return stats, rerr
					}
					continue
				}
				if email == "" {
					if rerr := markFailed(ctx, st, d, cfg, fmt.Errorf("recipient user %s not found or has no email", tuStr)); rerr != nil {
						return stats, rerr
					}
					continue
				}
				task.raw["to"] = email
				delete(task.raw, "toUserId")
				task.strings = toStr(task.raw)
			}
		}
		stats.NotificationsReady++

		if cfg.DryRun {
			dryClaimed = append(dryClaimed, d.ID)
			stats.DryRunProcessed++
			stats.DryRunWouldHaveSent++
			continue
		}

		projectName, _ := task.strings["projectName"]
		projectId, _ := task.strings["projectId"]
		isComment, _ := task.raw["isComment"].(bool)
		r, rerr := render(s, task.emailType, projectName, projectId, isComment)
		if rerr != nil {
			if merr := markFailed(ctx, st, d, cfg, rerr); merr != nil {
				return stats, merr
			}
			continue
		}

		if serr := mail.Send(task.strings["to"], r.Subject, r.Text, r.HTML); serr != nil {
			if merr := markFailed(ctx, st, d, cfg, serr); merr != nil {
				return stats, merr
			}
			continue
		}
		if delErr := st.DeleteDoc(ctx, d.ID); delErr != nil {
			return stats, delErr
		}
		stats.EmailsSent++
	}

	if cfg.DryRun && len(dryClaimed) > 0 {
		if rerr := st.ReleaseMany(ctx, dryClaimed); rerr != nil {
			return stats, rerr
		}
	}
	return stats, nil
}

// markFailed — node `_markFailed` (attempts counter + dead/backoff split).
func markFailed(ctx context.Context, st Store, d *Doc, cfg Config, err error) error {
	attempts := 1 // fresh claim: doc may carry attempts from prior failures
	if d.Attempts > 0 {
		attempts = d.Attempts + 1
	}
	if attempts >= cfg.MaxAttempts {
		return st.MarkDead(ctx, d.ID, attempts, errMessage(err))
	}
	backoff := cfg.BackoffBase * (1 << uint(attempts-1))
	return st.MarkRetry(ctx, d.ID, attempts, time.Now().Add(backoff), errMessage(err))
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
