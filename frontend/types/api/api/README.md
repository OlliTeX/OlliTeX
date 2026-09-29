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

> 2026-09-25: `notifications.d.ts` moved to `frontend/types/api/notifications.d.ts`
> — its Node-side consumers (modules/notifications app tree + cron entry) were
> deleted (ARC-8 cronmail port + notifications severance); the one live
> consumer (`frontend/js/features/ide-settings/hooks/use-project-notification-
> preferences.ts`) imports it directly. The file itself is unchanged.

Note: `isPrivilegeUpgrade` is only *declared* here; its runtime implementation
is `app/src/Features/Authorization/PrivilegeLevels.mjs` (Node backend).
