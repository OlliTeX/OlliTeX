# `githubinterface` (Go) — the GitHub sync bridge (token-protected)

> 1:1 Go drop-in for **`services/githubinterface`** (Node). Internal bridge
> between Overleaf and GitHub (clone / push / branch ops), authenticated with
> the shared service token (`go/pbhttp.RequireServiceToken`).

## What it does

Backs the "connect to GitHub" project sync: list/verify accounts and repos,
create a repo, clone the default branch into the project workdir, read
commits/branches, push/pull with conflict detection, and report branch state.
It talks to GitHub **both over the HTTPS API** (REST, `/api/v3` & `/api/v4`
surfaces) **and over the git protocol** (`GIT_URL` clone/fetch/push) — the Node
service does the same split, and the Go port keeps it (`client.go` REST vs
`git.go` transport, with the same per-op limits: `GITHUBINTERFACE_MAX_OPS` and
a working-dir root `GITHUBINTERFACE_WORKDIR_ROOT` under
`/var/lib/overleaf/tmp/...`-style paths).

| Route | Purpose |
| --- | --- |
| `/check` | verify account + repo access |
| `/list-repos` / `/orgs` | enumerate repos / organisations |
| `/create-repo` | create a repo for the project |
| `/clone` | clone default branch into the workdir |
| `/commits` / `/branch-head` | read commit list / branch head SHA |
| `/log` | commit log details |
| `/can-push` | is push allowed (up-to-date / perms) |
| `/push` / `/pull` | push / pull with conflict semantics |
| `/health`, `/status` | liveness |

## 1:1 mapping

| Go file | Node original |
| --- | --- |
| `handler.go` | route handlers + token gate |
| `rest.go` | the GitHub REST client |
| `git.go` | the git-protocol transport (clone/fetch/push) |
| `client.go` | combined client / config |
| `helpers.go` | path + state helpers |
| `errors.go` | error → status mapping (incl. the 409/503 shapes) |
| `*_test.go` | the locked contract |

## Interface

- **Port** — `GITHUBINTERFACE_PORT` (no default; set by the CE compose)
- **Env** — `GITHUBINTERFACE_PORT`, `GITHUBINTERFACE_MAX_OPS`,
  `GITHUBINTERFACE_WORKDIR_ROOT`, `SHARED_SERVICE_TOKEN`
- **Auth** — shared service token (constant-time) + the user's GitHub token
  passed per request, exactly as the Node service does

## Build / test / run

```sh
go build ./go/services/githubinterface
go test  ./go/services/githubinterface
go run   ./cmd/githubinterface
```

## Status

Converted, tests green. Cutover in the CE image awaits the owner call.
