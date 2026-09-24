# types/api — canonical shared (backend↔frontend) TypeScript types

Homes the self-contained type declarations that the React frontend and the
Node backend both imported — relocated out of the Node backend during the
P7 Node-retirement reorg so the backend can be junked without dangling type
imports (oracle-safe: the old locations are now re-export shims that point
here).

| file                  | canonical exports                                                        | moved from (shim remains)                                  |
|-----------------------|--------------------------------------------------------------------------|------------------------------------------------------------|
| `tags.d.ts`           | `Tag`                                                                    | `app/src/Features/Tags/types.d.ts`                          |
| `authorization.d.ts`  | `Source(s)`, `PrivilegeLevel(s)`, `PublicAccessLevel(s)` + type decls    | `app/src/Features/Authorization/types.d.ts`                 |
| `notifications.d.ts`  | `NotificationPreferencesSchema`, `GlobalNotificationPreferencesSchema`   | `modules/notifications/app/src/types.d.ts`                  |

Note: `isPrivilegeUpgrade` is only *declared* here; its runtime implementation
is `app/src/Features/Authorization/PrivilegeLevels.mjs` (Node backend).
