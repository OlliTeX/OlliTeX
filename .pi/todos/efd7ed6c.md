{
  "id": "efd7ed6c",
  "title": "[V] Add git-bridge (Java) compile to server-ce Makefile via builder container",
  "tags": [
    "server-ce",
    "git-bridge",
    "makefile",
    "order-5"
  ],
  "status": "done",
  "created_at": 1788360827618,
  "last_change": 1788405583630,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Task
Add git-bridge (JAVA service, services/git-bridge, package uk.ac.ic.wlgitbridge) compilation to /data_1/image_mining/the_diff/overleaf/server-ce Makefile.

## Constraints
- No external/official git-bridge docker image available — must build from source in-repo.
- Don't force users to have local Java build infra → use a builder container (e.g. maven/openjdk image) stage in the Makefile/docker build, similar to how other CE services are handled; check services/git-bridge for its build system (pom.xml / gradle) first.
- The running git-bridge container in the shared deploy currently uses quay.io/sharelatex/git-bridge:6.2.0 — the built artifact must replace that (tag the built image consistently, wire compose to it).

## Check first
ls services/git-bridge; find build tool (mvn/gradle/ant); its Dockerfile (if any) in the source branch; runtime user/port (8000) + ROOT_DIR mount; how the CE nginx template proxies /git/ (overleaf.conf.template already in place from [0]).

## Done when
`make all` (or the new make target) produces a git-bridge image from source without host Java toolchain; compose wiring points to it; image starts and serves /api.
