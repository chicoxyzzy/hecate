# Deployment

The [Quick Start](../README.md#quick-start) covers `docker compose up` end-to-end. This page is the reference for everything past the first run: pinning images, optional services, recovering a lost admin token, and resetting state.

## Image pinning

`docker-compose.yml` references `ghcr.io/chicoxyzzy/hecate:latest`, a multi-arch (`linux/amd64`, `linux/arm64`) image published from this repo on every `v*` tag. A fresh host can `docker compose pull` and start without a build step.

To pin to a specific release, replace `:latest` with `:vX.Y.Z` in `docker-compose.yml`. Pinning is recommended for production — `:latest` floats over major-version bumps that may include schema or config changes.

When the working tree is a checkout of the source, `docker compose up` rebuilds locally from the bundled `Dockerfile` instead of pulling. Useful for testing changes; remove the `image:` line or run `docker compose build` first if you want the local build to be the canonical artifact.

## Optional services (compose profiles)

```bash
docker compose --profile postgres up    # adds Postgres on :5432 for durable state
docker compose --profile ollama up      # adds Ollama on :11434 for local models
docker compose --profile full up        # both of the above
```

Profiles are additive — `--profile postgres --profile ollama` works too. Profiles are off by default so a bare `docker compose up` stays "just the gateway" with no extra containers.

To use the Postgres profile across subsystems, point each backend at it via env vars in `.env`:

```bash
GATEWAY_CONTROL_PLANE_BACKEND=postgres
GATEWAY_TASKS_BACKEND=postgres
# ... etc, see Storage backends in the README
POSTGRES_DSN=postgres://hecate:hecate@postgres:5432/hecate?sslmode=disable
```

## Recovering a lost admin token

The first-run banner is the easiest path. If it's scrolled out of `docker compose logs`, the token also lives in the bootstrap file on the `hecate-data` volume. The gateway image is distroless (no shell), so use `docker compose cp` to copy the file out:

```bash
docker compose cp hecate:/data/hecate.bootstrap.json - | tar -xO | jq -r .admin_token
```

(`docker compose cp ... -` emits a tar archive, hence the `tar -xO`.)

The bootstrap file (and the SQLite database, see below) persist across container restarts as long as the `hecate-data` volume sticks around — only `docker compose down -v` (or `make reset-docker`) wipes them.

## Resetting state

To wipe the stack back to first-run — removes the `hecate-data` (admin token + SQLite db), `postgres-data`, and `ollama-models` volumes and regenerates the admin token on the next `docker compose up`:

```bash
make reset-docker
```

The next page load in the browser detects the rejected stale token and re-prompts for the regenerated one — no manual `localStorage` cleanup needed.

For local (non-Docker) development resets, see [`development.md`](development.md#reset-state).

## Choosing a backend tier

Three tiers, picked per subsystem via `GATEWAY_*_BACKEND` env vars:

- **`memory`** — in-process, ephemeral. Right for tests and local iteration via the bare binary.
- **`sqlite`** — single-file durable store at `GATEWAY_SQLITE_PATH` (default `/data/hecate.db` in the docker image). **Default for the docker image** so `docker compose up` persists tenants / keys / pricebook / tasks / chat sessions across restarts without extra config. Right for single-node production.
- **`postgres`** — multi-node production. Required for the semantic cache (pgvector).

The semantic cache has no SQLite backend and stays on `memory` in the docker image — see the README's Storage backends section for the full matrix and the rationale.

To opt out of SQLite persistence in docker (e.g. ephemeral test stack), set `GATEWAY_*_BACKEND=memory` for the subsystems you want ephemeral, either in `.env` or via compose env overrides.
