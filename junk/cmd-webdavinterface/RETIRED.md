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
