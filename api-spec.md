# Live Workspace — API Specification (v1)

Backend: **TBD** (Go / Node / etc.). This document is the **contract** the frontend
is built against. The frontend currently runs **fully client-side** — the shared
document lives in `localStorage` and syncs across tabs via `BroadcastChannel`
(`src/lib/sync.ts`). Implementing this API replaces that local layer with real
multi-user, multi-device persistence; the wire shapes below mirror the frontend
domain types in `src/lib/types.ts` (snake_case on the wire, camelCase in the app —
the service layer normalizes).

> **What the backend owns:** persistence of the workspace document (resources,
> fields, comments, activity), identity/presence, and broadcasting changes.
> **What stays client-side:** code generation (TypeScript / JSON mock) is computed
> in the browser from the schema (`src/lib/codegen.ts`) — no endpoint needed.

---

## 1. Conventions

### Base path
```
/api/v1
```
Configured via `NEXT_PUBLIC_API_URL` (default `http://localhost:8080/api/v1`).

### Authentication
- Create a permanent room with `POST /rooms`, or join one with `POST /rooms/join`.
- Both endpoints return a **Bearer JWT** scoped to the room and collaborator:
  ```
  Authorization: Bearer <access_token>
  ```
- The token resolves the current **Collaborator** (the "me" identity) and room.
  A token cannot read or mutate another room.

### Timestamps
All timestamps are ISO-8601 UTC (`2026-06-30T08:00:00Z`). Presence heartbeats may
additionally carry an epoch-ms `ts` for TTL math.

### Common response envelope
Every response uses the same envelope (frontend `unwrap()` returns `data`):
```json
{ "success": true, "message": "OK", "data": { } }
```

### Error format
```json
{
  "success": false,
  "message": "Field key already exists on this resource",
  "data": null,
  "error": {
    "code": "VALIDATION_ERROR",
    "details": { "key": "email" }
  }
}
```
HTTP status mirrors the class of error (400/401/403/404/409/422/500).

| code | meaning |
|------|---------|
| `VALIDATION_ERROR` | malformed body / duplicate key / bad enum value |
| `NOT_FOUND` | unknown resource, field, or comment id |
| `REV_CONFLICT` | optimistic-concurrency rev mismatch (stale write) |
| `UNAUTHORIZED` | missing/invalid token |
| `FORBIDDEN` | authenticated but not allowed |

### Optimistic concurrency (`rev`)
The workspace document carries a monotonically increasing integer `rev`. Every
mutation **bumps `rev` by 1** and returns the new value. Mutating requests **may**
send the `rev` they last saw via header `If-Match: <rev>`; if it is stale the
server responds `409 REV_CONFLICT` with the current `data` snapshot so the client
can rebase. This mirrors the client's existing `rev`-based merge in `store.ts`.

### Pagination
List endpoints accept `?page=1&limit=50` and return `data.items` + `data.page_info`
(`{ "page": 1, "limit": 50, "total": 84 }`).

### Real-time (WebSocket) — REQUIRED for collaboration
Replaces the current `BroadcastChannel` layer. See §4. Without it the app degrades
to request/response (no live presence, manual refetch).

---

## 2. Data Models

### Collaborator
```json
{
  "id": "col_ava",
  "name": "Ava Chen",
  "role": "backend",         // backend | frontend
  "color": "#2563EB"         // hex, for avatars/cursors
}
```

### SchemaField
```json
{
  "id": "fld_email",
  "key": "email",
  "type": "string",          // see Field Types
  "required": true,
  "state": "ready",          // draft | ready | breaking
  "change": "stable",        // stable | added | modified | removed
  "description": "Primary login email", // optional, may be omitted/null
  "value": null              // optional; only for type "json" — nested JSON shape/sample
}
```

**Field Types** (`type`)
| type | TS mapping | JSON-mock sample |
|------|-----------|------------------|
| `string` | `string` | `"sample_<key>"` |
| `number` | `number` | `0` |
| `boolean` | `boolean` | `false` |
| `uuid` | `string` | `"0000...-...0000"` |
| `timestamp` | `string` | ISO-8601 |
| `json` | inferred from `value` (else `Record<string, unknown>`) | `value` (else `{}`) |
| `string[]` | `string[]` | `["sample"]` |
| `number[]` | `number[]` | `[0]` |
| `enum` | `string` | first token of `description` |

**State** (`state`) — `draft` (work in progress) · `ready` (agreed/stable) ·
`breaking` (a change that breaks existing clients). Drives the `[Draft] [Ready]
[Breaking Change]` badges.

**Change** (`change`) — diff status vs the last agreed version, drives the
blueprint's line-weight/colour: `added` (green) · `modified` (amber) · `removed`
(red, soft-deleted — kept for the diff but excluded from generated code) ·
`stable` (no diff).

### Resource
A unit in the explorer: an API endpoint, a database table, or a schema model.
```json
{
  "id": "res_create_order",
  "name": "createOrder",
  "kind": "endpoint",        // endpoint | database | model
  "method": "POST",          // endpoints only: GET|POST|PUT|PATCH|DELETE (null otherwise)
  "path": "/api/v1/orders",  // endpoints only (null otherwise)
  "state": "breaking",       // rollup = worst state among live (non-removed) fields
  "fields": [ "...SchemaField" ],
  "updated_at": "2026-06-30T08:00:00Z",
  "updated_by": "Noah Reed"  // collaborator display name
}
```
> `state` is **server-derived** from the fields (breaking > draft > ready). Clients
> display it; they do not author it directly. `updated_at` / `updated_by` are set
> by the server on every mutation.

### ResponseSchema (frontend-local)
The per-status response shapes shown **below the request body** of the API Endpoint
view as a **status tab strip** (`200` / `400` / `500` …) — each tab opens the same
Visual / JSON Schema / Example JSON / Example TypeScript editor as the request body.
The backend `Resource` model has **no slot for these yet**, so the frontend keeps
them client-side, persisted in `localStorage`
(`live-workspace:response-schemas`, keyed by `resource.id`) — see
`src/lib/responseSchemas.ts`. They are populated manually or by the **spec import**
flow (`src/lib/specImport.ts`, OpenAPI YAML/JSON or Postman collection).
```json
{
  "status": 200,                 // HTTP status; 0 = OpenAPI "default"
  "description": "OK",           // optional short label
  "fields": [ "...SchemaField" ] // same SchemaField shape as the request blueprint
}
```
> **When the backend adopts these:** add a `responses: ResponseSchema[]` array to
> `Resource` (snake_case on the wire), normalize it in
> `src/services/workspace.service.ts`, and the local store becomes a cache/fallback.

### Bookmark (frontend-local)
The explorer pins bookmarked resources in a **"Bookmarked" group at the top**.
Bookmarks are a per-user preference with **no backend slot**, so they live entirely
client-side as a set of `resource.id`s in `localStorage`
(`live-workspace:bookmarks`) — see `src/lib/bookmarks.ts`.
> **When the backend adopts these:** store per-collaborator (e.g.
> `Collaborator.bookmarks: string[]` or a `GET/PUT /me/bookmarks` endpoint); the
> local store then becomes a cache/fallback.

### EndpointStatus (frontend-local)
A per-endpoint **workflow/progress** status shown as a dropdown pill in the endpoint
header. It is **distinct from `Resource.state`** (the server-derived field rollup
`draft | ready | breaking`): this tracks how far the endpoint is in the build
pipeline. The backend `Resource` model has **no slot for it yet**, so — like
bookmarks and response schemas — it lives entirely client-side in `localStorage`
(`live-workspace:endpoint-status`, keyed by `resource.id`) — see
`src/lib/endpointStatus.ts`. Only meaningful for `kind:"endpoint"`; defaults to
`draft`.
```
draft        // not started / spec only
inprogress   // being implemented
testing       // implemented, under test (incl. E2E flows)
done         // shipped / verified
```
> **When the backend adopts this:** add a `status` field to `Resource` (endpoints
> only; snake_case on the wire) settable via `PATCH /resources/{id}`, normalize it
> in `src/services/workspace.service.ts`, and the local store becomes a
> cache/fallback.

### Comment
Inline discussion, optionally anchored to a single field.
```json
{
  "id": "cmt_8842",
  "resource_id": "res_create_order",
  "field_id": "fld_currency",   // null = comment on the whole resource
  "author": "Liam Park",
  "role": "frontend",           // author's role at post time
  "body": "This breaks the web client — can we default to USD for one release?",
  "at": "2026-06-30T07:58:10Z"
}
```

### ActivityEvent
Append-only audit feed. Emitted by the server on every mutation.
```json
{
  "id": "act_551",
  "actor": "Ava Chen",
  "verb": "added",              // added|edited|removed|renamed|set draft|set ready|set breaking|flagged|created|commented on
  "target": "avatarUrl",       // field key, "old → new", or resource name
  "resource_id": "res_user",
  "at": "2026-06-30T07:55:00Z"
}
```

### WorkspaceSnapshot
Everything needed to hydrate the UI in one call (mirrors the client `WorkspaceDoc`
plus the roster).
```json
{
  "rev": 42,
  "workspace_id": "wsp_demo",
  "resources": [ "...Resource" ],
  "comments": [ "...Comment" ],
  "activity": [ "...ActivityEvent" ],
  "collaborators": [ "...Collaborator" ],
  "server_time": "2026-06-30T08:00:00Z"
}
```

### Presence (real-time only — not persisted)
```json
{
  "client_id": "c_9f3a2b",      // per-tab id
  "collaborator_id": "col_ava",
  "ts": 1782547200000           // epoch ms of last heartbeat; online if within TTL (~8s)
}
```

### API test request/result (not persisted)
Backing shape for the in-endpoint **"Try it"** helper. The request is proxied
**server-side** (`POST /http/test`) so the browser isn't blocked by CORS and
latency is measured on the server. Nothing is persisted; the draft lives
client-side in `localStorage` (`live-workspace:api-tests`, keyed by `resource.id`)
— see `src/lib/apiTester.ts`.
```json
// request
{ "method": "POST", "url": "http://localhost:8080/api/v1/users?limit=5",
  "headers": { "Content-Type": "application/json" }, "body": "{\"name\":\"Ada\"}" }
// result
{ "status": 201, "duration_ms": 42, "headers": { "Content-Type": ["application/json"] },
  "body": "{...}", "size": 128, "truncated": false, "error": "" }
```
On a transport failure (DNS/refused/timeout) the endpoint still returns `200` with
`status: 0` and a non-empty `error` so the UI can render it inline.

### FlowDefinition (E2E Flow Testing — persisted)
A workflow parsed from an uploaded **Arazzo (OpenAPI Workflows) JSON/YAML** file.
Stored in its **own Mongo collection** (`flows`), scoped by `workspace_id` — *not*
embedded in the rev-guarded workspace document, so runs never conflict with schema
edits. A step resolves its HTTP call from an explicit `method`+`path`, or from its
`operation_id` matched (case-insensitive, by name) against the workspace's endpoint
resources — the integration point that ties flows to the shared API spec.
```json
{
  "id": "flw_1a2b", "workspace_id": "123456",
  "name": "loginAndFetch", "description": "Log in then fetch the profile",
  "inputs": [ { "name": "username", "in": "input", "value": "alice" } ],
  "steps": [
    {
      "id": "fst_9f", "step_id": "login", "description": "…",
      "operation_id": "loginUser", "method": "POST", "path": "/login", "order": 0,
      "depends_on": [],                              // inferred from $steps.<id> refs or explicit
      "parameters": [ { "name": "q", "in": "query", "value": "$inputs.username" } ],
      "request_body": { "password": "$inputs.password" },
      "outputs": [ { "name": "token", "from": "$response.body#/token" } ],
      "success_criteria": [ { "condition": "$statusCode == 200", "context": "", "type": "" } ]
    }
  ],
  "created_at": "2026-07-01T08:00:00Z", "created_by": "Ava Chen"
}
```

### FlowRun (E2E Flow Testing — persisted)
The result of executing a `FlowDefinition` for real against a `base_url`. Stored in
the `flow_runs` collection. Steps run in dependency order; outputs chain into later
steps; the first failing/erroring step stops the run and the rest are `skipped`.
```json
{
  "id": "run_77", "flow_id": "flw_1a2b", "workspace_id": "123456",
  "status": "passed",                     // passed | failed | errored
  "base_url": "http://localhost:8080", "started_at": "…", "finished_at": "…",
  "steps": [
    { "step_id": "login", "method": "POST", "url": "http://localhost:8080/login",
      "status": 200, "duration_ms": 42, "passed": true, "skipped": false,
      "failures": [], "outputs": { "token": "abc" }, "request_body": "{…}", "response": "{…}" }
  ]
}
```
Supported runtime expressions: `$statusCode`, `$response.body`,
`$response.body#/json/pointer`, `$response.header.Name`, `$inputs.name`,
`$steps.<stepId>.outputs.<name>`. Success criteria support `==/!=/>/>=/</<=`
comparisons and `type: "regex"`; no criteria ⇒ a 2xx status passes.

---

## 3. REST Endpoints

> All paths relative to `/api/v1`; all responses use the §1 envelope. Mutations
> bump `rev`, set `updated_at`/`updated_by`, append an `ActivityEvent`, and push
> the change over WebSocket (§4).

### Rooms (public)
| method | path | purpose |
|--------|------|---------|
| POST | `/rooms` | Create a permanent room from `{ "name": "Alice" }` |
| POST | `/rooms/join` | Join or restore identity from `{ "room_code": "123456", "name": "Bob" }` |

Both responses contain `access_token`, `room_code`, `collaborator`, and `session`.
`session` is the complete `WorkspaceSnapshot`, including all persisted discussion.

### Workspace
| method | path | purpose |
|--------|------|---------|
| GET | `/workspace` | Full `WorkspaceSnapshot` (initial hydrate) |
| GET | `/workspace/collaborators` | Team roster (`Collaborator[]`) |
| GET | `/me` | The current authenticated `Collaborator` |

### Resources
| method | path | purpose |
|--------|------|---------|
| GET | `/resources` | List resources (`?kind=endpoint\|database\|model` optional) |
| GET | `/resources/{id}` | One resource (with fields) |
| POST | `/resources` | Create a resource |
| PATCH | `/resources/{id}` | Rename / set `method` / `path` |
| DELETE | `/resources/{id}` | Delete a resource |

### Fields
| method | path | purpose |
|--------|------|---------|
| POST | `/resources/{id}/fields` | Add a field |
| PATCH | `/resources/{id}/fields/{field_id}` | Update key/type/required/state/description |
| DELETE | `/resources/{id}/fields/{field_id}` | Remove a field (see soft-delete rule) |

### Comments
| method | path | purpose |
|--------|------|---------|
| GET | `/resources/{id}/comments` | Comments for a resource (`?field_id=` to filter) |
| POST | `/resources/{id}/comments` | Add a comment (optionally anchored to a field) |
| DELETE | `/comments/{id}` | Delete a comment (author/admin) |

### Activity
| method | path | purpose |
|--------|------|---------|
| GET | `/activity` | Activity feed, newest first (`?limit=50&resource_id=` optional) |

### API testing (proxy)
| method | path | purpose |
|--------|------|---------|
| POST | `/http/test` | Proxy one outbound request; returns status/time/headers/body (see model) |

### E2E Flow Testing
| method | path | purpose |
|--------|------|---------|
| POST | `/flows/parse` | Parse an uploaded Arazzo file (multipart `file` or raw body) → preview `{ flows: FlowDefinition[] }`, **not** persisted |
| POST | `/flows` | Save a parsed `FlowDefinition` (scoped to the room) |
| GET | `/flows` | List saved flows for the room |
| GET | `/flows/{id}` | One flow definition |
| DELETE | `/flows/{id}` | Delete a saved flow and its run history |
| POST | `/flows/{id}/run` | Run the flow for real: `{ "base_url": "...", "inputs": { } }` → `FlowRun` |
| GET | `/flows/{id}/runs` | Run history (newest first, capped at 50) |
| GET | `/flows/runs/{run_id}` | One `FlowRun` result |

> Flows live in dedicated `flows` / `flow_runs` collections and do **not** bump the
> workspace `rev` or emit WebSocket/activity events. `DELETE /flows/{id}` hard-deletes
> the definition **and cascades** to its `flow_runs`; response `data`:
> `{ "flow_id": "flw_1a2b" }`.

---

## 4. Real-time (WebSocket)

```
GET /api/v1/stream      (HTTP upgrade; auth via ?token= or Authorization header)
```

### Client → server
| type | payload | purpose |
|------|---------|---------|
| `presence.heartbeat` | `{ "client_id", "collaborator_id" }` | keep-alive every ~3s |
| `presence.leave` | `{ "client_id" }` | sent on tab close |

### Server → client
All payloads reuse the §2 models. Clients merge by `rev` (ignore `rev <= local`).

| type | payload |
|------|---------|
| `snapshot` | `WorkspaceSnapshot` (sent on connect) |
| `resource.created` / `resource.updated` / `resource.deleted` | `{ "rev", "resource": Resource }` (deleted → `{ "rev", "resource_id" }`) |
| `field.created` / `field.updated` / `field.removed` | `{ "rev", "resource": Resource }` (send the whole updated resource so `state` rollup + fields stay consistent) |
| `comment.created` / `comment.deleted` | `{ "rev", "comment": Comment }` / `{ "rev", "comment_id" }` |
| `activity.created` | `{ "activity": ActivityEvent }` |
| `presence.update` | `Presence` (a peer's heartbeat) |
| `presence.leave` | `{ "client_id" }` |

> Online roster = collaborators with a `Presence` heartbeat newer than the TTL
> (~8s). The server prunes stale beacons and emits `presence.leave`.

---

## 5. Examples

### GET `/workspace`
```json
{
  "success": true, "message": "OK",
  "data": {
    "rev": 42, "workspace_id": "wsp_demo",
    "resources": [
      {
        "id": "res_user", "name": "User", "kind": "model",
        "method": null, "path": null, "state": "ready",
        "fields": [
          { "id": "fld_id", "key": "id", "type": "uuid", "required": true,
            "state": "ready", "change": "stable" },
          { "id": "fld_avatar", "key": "avatarUrl", "type": "string",
            "required": false, "state": "draft", "change": "added",
            "description": "Proposed — CDN URL" }
        ],
        "updated_at": "2026-06-30T07:52:00Z", "updated_by": "Ava Chen"
      }
    ],
    "comments": [ "...Comment" ],
    "activity": [ "...ActivityEvent" ],
    "collaborators": [ "...Collaborator" ],
    "server_time": "2026-06-30T08:00:00Z"
  }
}
```

### POST `/resources`
Request:
```json
{ "name": "createOrder", "kind": "endpoint", "method": "POST", "path": "/api/v1/orders" }
```
Response `data`: the created `Resource` (server seeds an `id` field, `state:"draft"`).

### PATCH `/resources/{id}`
Request (any subset):
```json
{ "name": "createOrderV2", "path": "/api/v1/v2/orders" }
```
Response `data`: `{ "rev": 43, "resource": { "...Resource" } }`

> The `path` and `method` are editable for the lifetime of an endpoint — a newly
> created endpoint is seeded with `method:"GET"`, `path:"/api/v1/new"` and the
> client renames/repaths it via this `PATCH`. There is **no** reserved or
> immutable path; `/api/v1/new` is just a placeholder default.

### DELETE `/resources/{id}`
Permanently removes a resource (endpoint / database / model) and all of its
fields. Hard delete — the resource is gone, not soft-flagged (unlike a field's
soft-delete rule). Emits a `resource.deleted` over WebSocket (§4) and an
`ActivityEvent` (`verb:"removed"`).

Response `data`: `{ "rev": 45, "resource_id": "res_create_order" }`

### POST `/resources/{id}/fields`
Request:
```json
{ "key": "couponCode", "type": "string", "required": false }
```
- `state` defaults to `draft`, `change` to `added`.
- `409 VALIDATION_ERROR` if `key` already exists on the resource.

Response `data`: `{ "rev": 44, "resource": { "...Resource" } }` (full resource so
the client refreshes the `state` rollup).

### PATCH `/resources/{id}/fields/{field_id}`
Request (any subset of `key`, `type`, `required`, `state`, `description`, `value`):
```json
{ "type": "number", "state": "breaking", "description": "now integer cents" }
```
- Editing a field that was `change:"stable"` flips it to `change:"modified"`; an
  `added` field stays `added`.
- Response `data`: `{ "rev", "resource": { "...Resource" } }`

### DELETE `/resources/{id}/fields/{field_id}`
**Soft-delete rule (matches the client):**
- If the field's `change` is `added` (never shipped) → hard-remove it.
- Otherwise → set `change:"removed"` + `state:"breaking"` (kept in the diff,
  excluded from generated code).

Response `data`: `{ "rev", "resource": { "...Resource" } }`

### POST `/resources/{id}/comments`
Request:
```json
{ "field_id": "fld_currency", "body": "Can we default to USD for one release?" }
```
`author`/`role`/`at` are set server-side from the token. `field_id` optional.

Response `data`: `{ "rev", "comment": { "...Comment" } }`

### GET `/activity?limit=50`
```json
{ "success": true, "message": "OK",
  "data": { "items": [ "...ActivityEvent" ],
            "page_info": { "page": 1, "limit": 50, "total": 128 } } }
```

---

## 6. Frontend rendering contract (what the UI needs)

| UI element | source field(s) |
|------------|-----------------|
| Left explorer groups | `Resource.kind` (`endpoint` / `database` / `model`) |
| Endpoint method tag + path | `Resource.method`, `Resource.path` |
| Resource state dot / badge | `Resource.state` (server rollup) |
| Endpoint status pill (Draft / In Progress / Testing / Done) | `EndpointStatus` (frontend-local, see §2) |
| Blueprint field row | `SchemaField` (`key`, `type`, `required`, `description`) |
| `[Draft] [Ready] [Breaking Change]` badge | `SchemaField.state` |
| Diff line-weight / colour | `SchemaField.change` |
| Field comment count | `Comment[]` where `resource_id` + `field_id` match |
| "updated 2m ago by X" | `Resource.updated_at`, `updated_by` |
| Request body tabs (Visual / JSON Schema / Example JSON / Example TypeScript) | `Resource.fields` → client-side schema tree (`schemaConvert.ts`) — **no API** |
| Response tabs (per HTTP status) | `ResponseSchema[]` (frontend-local, see §2) |
| Explorer "Bookmarked" group (pinned top) | client-side bookmark set (frontend-local, see §2) |
| Right · Activity tab | `ActivityEvent[]` (newest first) |
| Right · Comments tab | `Comment[]` (filtered by selected resource / focused field) |
| Top bar presence avatars | `Collaborator[]` + live `Presence` (online if heartbeat within TTL) |
| Top bar "Import API" | parses an OpenAPI/Postman spec and **`POST /resources`** one endpoint per chosen operation |

The client treats each read/WS payload as the **single source of truth**, merges
mutations by `rev` (last-writer-wins on conflict, with `409` rebase), and never
authors `state` rollups, `updated_at`, or `activity` — those are server-owned.
```
