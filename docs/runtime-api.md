# Runtime API Notes

Hecate exposes a coding-runtime API surface under `/v1/tasks` for client-orchestrated agents.

## Core resources

- `task`
- `task_run`
- `task_step`
- `task_artifact`
- `task_approval`
- `task_run_event`

## Lifecycle endpoints

- `POST /v1/tasks`
- `GET /v1/tasks`
- `GET /v1/tasks/{id}`
- `POST /v1/tasks/{id}/start`
- `POST /v1/tasks/{id}/runs/{run_id}/retry`
- `POST /v1/tasks/{id}/runs/{run_id}/resume`
- `POST /v1/tasks/{id}/runs/{run_id}/cancel`

## Execution detail endpoints

- `GET /v1/tasks/{id}/runs`
- `GET /v1/tasks/{id}/runs/{run_id}`
- `GET /v1/tasks/{id}/runs/{run_id}/steps`
- `GET /v1/tasks/{id}/runs/{run_id}/steps/{step_id}`
- `GET /v1/tasks/{id}/runs/{run_id}/artifacts`
- `GET /v1/tasks/{id}/runs/{run_id}/artifacts/{artifact_id}`
- `GET /v1/tasks/{id}/artifacts`

## Approval endpoints

- `GET /v1/tasks/{id}/approvals`
- `GET /v1/tasks/{id}/approvals/{approval_id}`
- `POST /v1/tasks/{id}/approvals/{approval_id}/resolve`

## Event and stream endpoints

- `GET /v1/tasks/{id}/runs/{run_id}/events?after_sequence=<n>`
- `POST /v1/tasks/{id}/runs/{run_id}/events`
- `GET /v1/tasks/{id}/runs/{run_id}/stream?after_sequence=<n>`

Stream resume also supports `Last-Event-ID`.

## Runtime backend and queue configuration

- `GATEWAY_TASKS_BACKEND=memory|postgres`
- `GATEWAY_TASK_APPROVAL_POLICIES=shell_exec,git_exec,file_write,network_egress`
- `GATEWAY_TASK_QUEUE_BACKEND=memory|postgres`
- `GATEWAY_TASK_QUEUE_WORKERS=<int>`
- `GATEWAY_TASK_QUEUE_BUFFER=<int>`
- `GATEWAY_TASK_QUEUE_LEASE_SECONDS=<int>`
- `GATEWAY_TASK_MAX_CONCURRENT_PER_TENANT=<int>` (`0` disables the limit)

When `GATEWAY_TASKS_BACKEND=postgres`, tasks/runs/steps/approvals/artifacts/run-events are persisted and the stream replay cursor is durable across restarts. When `GATEWAY_TASK_QUEUE_BACKEND=postgres`, workers claim queue items with renewable leases, so pending runs survive process restarts and can be recovered by another worker when a lease expires.
