# Upgrades (installation)

Goal: move to a new image without losing data.

## The dance

1. **Build** the new image from the target tag/branch:
   ```bash
   cd server-ce
   make all
   ```
2. **Cycle** the server container (compose style):
   ```bash
   cd <your-compose-dir>
   sh cycle_overleafserver.sh     # recreate the overleafserver container on the new image
   ```
3. **Poll health** until `healthy` (20–40 s):
   ```bash
   docker inspect --format '{{.State.Health.Status}}' overleafserver
   ```
4. **Smoke-test**: login, open a project, recompile, download the PDF.

## Data

- Mongo and the filestore are **volumes** — a container cycle does not touch
  them. Newer images may migrate the Mongo schema in place.
- Snapshot Mongo + filestore **before** the cycle if you want a rollback
  point.

## Rollback

Keep the previous image tag locally (`docker images | grep sharelatex`) and
repeat the cycle with the old tag. The schema migrations in this fork are
forward-compatible for the supported pair of versions.

***

Verified against: OlliTeX @ `f827b5ceb9` (2026-09-16)
