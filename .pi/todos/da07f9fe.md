{
  "id": "da07f9fe",
  "title": "ALPINE CUTOVER (owner directive 2026-09-28): if e2e gate green → alpine:3.24 becomes default base, ubuntu version RETIRED; smaller is cleaner",
  "tags": [
    "goal",
    "buildsystem",
    "alpine",
    "owner-approved"
  ],
  "status": "done",
  "created_at": 1790594446794,
  "last_change": 1790665067681,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

DONE + PUSHED (commit ccbea1d963, 2026-09-28):
ALPINE CUTOVER complete per owner directive:
1. E2E gate GREEN on live stack (alpine:musl): smoke (login→create→editor→compile→PDF via sandbox), u101 history contract + 2-run stability, hub-storage-admin, collab-yjs, realtime-bus, consent. Flakes = CE login-throttle artifacts only (global-setup.ts now backoffs 70s + retries on no-form throttle page).
2. SIZE: ollitex/ollitex:main 6.41GB (alpine) < 6.7GB (ubuntu); base 693MB < 1.17GB.
3. Cutover: server-ce/Dockerfile-base = alpine:3.24 canonical; GO_BUILDER_TAG default = alpine builder (Makefile + Dockerfile ARG); ubuntu base + ubuntu golang-builder retired to junk/ (README with restore procedure).
4. musl fixes in-tree (portable): util-linux (setpriv), bash, apk npm+corepack+yarn 4.18 (COREPACK_HOME parity), www-data user (alpine nginx pre-creates gid 82 group), service(8)→nginx -s reload best-effort, groupadd/usermod→dual-branch in CLSI run scripts.
5. HAProxy edge package also shipped (server-ce/edge/ + ACME location in overleaf.conf.template) — owner-applied per edge/README.md.
6. Default `make all` verified green; e2e recreated on default :main (6.41GB) and gated.
7. WEB_GO_STATE.md §ALPINE CUTOVER ledgered.
