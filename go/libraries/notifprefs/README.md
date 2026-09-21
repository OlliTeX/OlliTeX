# `go/libraries/notifprefs` — notification-preferences contract

Go 1:1 port of **`libraries/notification-preferences`** (npm
`@overleaf/notification-preferences` v0.1.0): the **shared contract** for
Overleaf notification preferences — the 12-key per-project schema plus the
global mute/delay shape — used by *both* the web `notifications` module and
the chat service, so the schema + normalization live in exactly one place.

## Storage shape (single collection `notificationsPreferences`)
```
per project: { user_id, project_id, <ProjectPreferences> }        // 12 booleans
global:      { user_id, project_id: null,
               MuteAllNotifications, NotificationDelayMinutes }
```

## The 12 per-project keys
`commentOnOwnProject`, `commentOnInvitedProject`, `repliesOnAuthoredThread`,
`repliesOnParticipatingThread`, `commentResolvedOnAuthoredThread`,
`commentResolvedOnParticipatingThread`, `commentReopenedOnAuthoredThread`,
`commentReopenedOnParticipatingThread`, `trackedChangesOnOwnProject`,
`trackedChangesOnInvitedProject`, `trackChangesAcceptedOnAuthoredChange`,
`trackChangesRejectedOnAuthoredChange`.

## The API
| Symbol | Purpose |
| --- | --- |
| `var ProjectPreferenceKeys []string` | the 12 key names (membership matters, order doesn't) |
| `func DefaultProjectPreferences() map[string]bool` | all 12 set to `true` (Node `defaultProjectPreferences()`) |
| `func NormalizeProjectPreferences(prefs map[string]any) map[string]bool` | per key: `true` when the key is **absent** (Node `=== undefined`); otherwise JS-truthiness of the value — so an explicit `null`/`0`/`""`/`false` all normalize to `false` (a present-but-falsy value is a real "off", distinct from a missing key) |
| `type GlobalPreferences` / `func NormalizeGlobalPreferences(prefs map[string]any) GlobalPreferences` | the global shape (`MuteAllNotifications` + `NotificationDelayMinutes`) |
| `func NormalizeGlobalDelayMinutes(value any) *int64` | normalize the delay-minutes to a non-negative int pointer (nil = unset) |

## Conventions / gotchas
- **Absent vs falsy is the crux.** A *missing* key means "use the default
  (`true`)" — that's the editor's default-on. A *present* falsy key (`null`,
  `0`, `""`, `false`) is an explicit *off*. The normalization reproduces Node's
  `x === undefined ? true : Boolean(x)` exactly.
- **`NotificationDelayMinutes` normalization** clamps/validates to a non-negative
  integer; a bad value → `nil` (unset), matching the Node guard.

## Testing & coverage
`go test ./go/libraries/notifprefs/ -count=1 -cover` — oracle-pinned to the Node
`notification-preferences` suite (absent-vs-falsy, every key, global mute + delay).
**Coverage: 69.2%.** ⚠️ *This is currently below the 85% gate (the package is
tiny, so a few uncovered normalization arms dominate the %).* Before the LIB-16
strict gate this is either raised (add the remaining branch tests) or explicitly
**waived in `HANDOFF.md`** — see the L16 gate playbook.

## Dependencies
Standard library only.
