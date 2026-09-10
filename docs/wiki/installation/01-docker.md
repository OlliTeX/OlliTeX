# Docker deployment (installation)

Goal: run an OlliTeX instance from the published image.

## Prerequisites

- Docker (20.10+) + Docker Compose v2 on the host.
- ~10 GB free disk (images) + whatever filestore growth you expect.

## One-off build & boot

From a checkout of this repository:

```bash
cd server-ce
make all                 # builds sharelatex/sharelatex:<branch> (+ the TeX Live base image)
```

The [`Dockerfile-base`](../../../server-ce/Dockerfile-base) builds the
`sharelatex/sharelatex-base` image (dependencies + TeX Live); the
[`Dockerfile`](../../../server-ce/Dockerfile) builds the application image
on top.

The included deployment example
([`develop/docker-compose.yml`](../../../develop/docker-compose.yml)) gives
you nginx + overleaf + mongo + redis on a network with a healthy
login page:

![A healthy instance answering on /login](../assets/installation/01-docker-healthy.png)

## Running

- **Compose** (production shape): the `compose_cep` style compose file sets
  `OVERLEAF_SITE_URL` (this value defines the CSRF origin — keep it the
  exact public URL) and the app name.
- **Health**: after `docker compose up -d`, poll the `overleafserver`
  container until `Health: healthy` (typically 20–40 s).

## Data

- Mongo holds users/projects/metadata; the filestore holds files.
- Back up **both** (`mongodump` + the filestore volume) — see
  [Configuration](02-configuration.md) for the env layout.

***

Verified against: OlliTeX @ `f827b5ceb9` (2026-09-16)
