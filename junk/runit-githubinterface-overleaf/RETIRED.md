RETIRED 2026-09-29 (TPDS -> web merge, owner-approved 2026-09-29, TODO-d414c964):
the dropbox/github/webdav interface bridges were absorbed into the Go web
services (features/dropbox, features/webdav, and the self-contained TPDS
github surface in features/projectlist tpds* routes). No live caller in the
Go stack (web never HTTP-calls these services; the Node caller tree is the
legacy services/web, which retires under the P7 step-4 owner decision).
Preserved here with git history (S5 junking pattern). The service code
packages remain under go/services/{dropboxinterface,githubinterface,webdavinterface}
as oracle references with their contract tests (webdav/dropbox body-limit
config-DB wiring included).

## 2026-10-02 (TPDS merge completion — TODO-1d4ab033 / Option A)
The 006 "git provider sync" revival (2026-10-01) of this standalone service
was folded into the web binary IN-PROCESS: the ghsync bridge client now calls
go/services/githubinterface ops directly (same *GHI surface, 1:1 semantics,
MaxOps semaphore preserved in-process). No :4013, no runit service, no
standalone binary. The 006 run script (WORKDIR_ROOT pin) is preserved as
run.2026-10-02-006-revival; the 006 re-added cmd/githubinterface/main.go was
byte-identical to the junked main.go and was removed.
