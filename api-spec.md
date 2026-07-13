# Live Workspace — API Specification (v1)

Backend: **Go / Gin / MongoDB**. This document is the **contract** the frontend
is built against. The backend owns room-scoped persistence, mutation semantics,
and real-time broadcasts; the wire shapes below mirror the frontend domain types
in `src/lib/types.ts` (snake_case on the wire, camelCase in the app — the service
layer normalizes).

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
`GET /activity` accepts `?page=1&limit=50` (max `limit=100`) and returns
`data.items` + `data.page_info` (`{ "page": 1, "limit": 50, "total": 84 }`).
Other list endpoints currently return arrays directly.

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
Recursive: `children` holds nested properties for a `type: "object"` field,
`items` holds the element schema for a `type: "array"` field. Leaf (scalar)
fields leave both out. Written by `POST/PATCH/DELETE /resources/{id}/fields[/{field_id}]`
(flat, top-level only) or, for the full nested tree, `PUT /resources/{id}/request-fields`
and `PUT /resources/{id}/responses` (§3).
```json
{
  "id": "fld_email",
  "key": "email",
  "type": "string",          // see Field Types
  "required": true,
  "nullable": false,         // optional, default false
  "state": "ready",          // draft | ready | breaking
  "change": "stable",        // stable | added | modified | removed
  "description": "Primary login email", // optional, may be omitted/null
  "value": null,             // optional; legacy — only for type "json"
  "example": null,           // optional sample value
  "default": null,           // optional default value
  "enum_values": null,       // optional; for type "enum"
  "validation": null,        // optional { min_length, max_length, minimum, maximum, pattern, format }
  "children": null,          // optional []SchemaField; for type "object"
  "items": null              // optional SchemaField; for type "array"
}
```

**Field Types** (`type`)
| type | TS mapping | JSON-mock sample |
|------|-----------|------------------|
| `string` | `string` | `"sample_<key>"` |
| `number` | `number` | `0` |
| `integer` | `number` | `0` |
| `boolean` | `boolean` | `false` |
| `uuid` | `string` | `"0000...-...0000"` |
| `timestamp` | `string` | ISO-8601 |
| `object` | nested type from `children` | built from `children` |
| `array` | nested type from `items` `[]` | `[<items sample>]` |
| `enum` | `string` | first entry of `enum_values` |
| `null` | `null` | `null` |
| `json` *(legacy)* | inferred from `value` (else `Record<string, unknown>`) | `value` (else `{}`) |
| `string[]` *(legacy)* | `string[]` | `["sample"]` |
| `number[]` *(legacy)* | `number[]` | `[0]` |

`json`/`string[]`/`number[]` are accepted for backward compatibility with
fields saved before nesting existed; new saves use `object`/`array` with
`children`/`items` instead.

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
  "status": "inprogress",    // endpoints only: workflow status — draft|inprogress|testing|done (null otherwise)
  "fields": [ "...SchemaField" ],
  "responses": [ "...ResponseSchema" ], // endpoints only: per-status response shapes (may be []/omitted)
  "updated_at": "2026-06-30T08:00:00Z",
  "updated_by": "Noah Reed"  // collaborator display name
}
```
> `state` is **server-derived** from the fields (breaking > draft > ready). Clients
> display it; they do not author it directly. `status` is the **client-authored**
> workflow/progress attribute (see EndpointStatus below) — endpoints only, settable
> via `PATCH /resources/{id}`, and used to filter the left explorer
> (`GET /resources?status=`). `responses` is the endpoint's per-status response
> schemas (see ResponseSchema below). `updated_at` / `updated_by` are set by the
> server on every mutation.

### ResponseSchema
The per-status response shapes shown **below the request body** of the API Endpoint
view as a **status tab strip** (`200` / `400` / `500` …) — each tab opens the same
Visual / JSON Schema / Example JSON / Example TypeScript editor as the request body.
Persisted on the backend as the `Resource.responses` array (endpoints only) and
synced over the WebSocket like the rest of the resource. Populated manually, or by
the **spec import** flow (`src/lib/specImport.ts`, OpenAPI YAML/JSON or Postman
collection). The frontend keeps a `localStorage` cache
(`live-workspace:response-schemas`, keyed by `resource.id`, see
`src/lib/responseSchemas.ts`) as an offline fallback — replaced by server data
whenever `Resource.responses` is present.
```json
{
  "status": 200,                 // HTTP status; 0 = OpenAPI "default"
  "description": "OK",           // optional short label
  "fields": [ "...SchemaField" ] // same SchemaField shape as the request blueprint
}
```
> Written via **`PUT /resources/{id}/responses`** (§3), which replaces the whole
> array. On the wire `Resource.responses` is snake_case-free (`status`,
> `description`, `fields`); it is normalized in `src/services/workspace.service.ts`.

### Bookmark (frontend-local)
The explorer pins bookmarked resources in a **"Bookmarked" group at the top**.
Bookmarks are a per-user preference with **no backend slot**, so they live entirely
client-side as a set of `resource.id`s in `localStorage`
(`live-workspace:bookmarks`) — see `src/lib/bookmarks.ts`.
> **When the backend adopts these:** store per-collaborator (e.g.
> `Collaborator.bookmarks: string[]` or a `GET/PUT /me/bookmarks` endpoint); the
> local store then becomes a cache/fallback.

### EndpointStatus
A per-endpoint **workflow/progress** status shown as a dropdown pill in the endpoint
header. It is **distinct from `Resource.state`** (the server-derived field rollup
`draft | ready | breaking`): this tracks how far the endpoint is in the build
pipeline. The backend persists it as `Resource.status` for `kind:"endpoint"` only.
Endpoint creation defaults it to `draft`; legacy endpoint rows without a stored
status are read back as `draft`.
```
draft        // not started / spec only
inprogress   // being implemented
testing       // implemented, under test (incl. E2E flows)
done         // shipped / verified
```
The **left explorer filters by this status** (chips: All / Draft / In Progress /
Testing / Done) — the filter is endpoint-only (databases/models have no workflow
status). It is settable via `PATCH /resources/{id}` and filterable via
`GET /resources?status=<value>` (§3).

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

### ChatMessage
Project-wide team chat, not anchored to any resource. Append-only and stored
outside the rev'd workspace aggregate — sending a message does **not** bump
`rev` and never hits a revision conflict.
```json
{
  "id": "msg_1a2b3c",
  "author_id": "col_ava",
  "author": "Ava Chen",
  "role": "backend",            // author's role at send time
  "body": "Deploying the staging API in 5 minutes 🚀",
  "at": "2026-06-30T08:01:00Z"
}
```

### TaskLog
A **manually-authored** backend work-update entry — the backend developer types
"I added X / changed Y / fixed Z" so the frontend can see, in one live feed, what
the backend has actually shipped. Distinct from `ActivityEvent` (which is the
server's automatic audit of schema mutations): a `TaskLog` is human-written prose.
Like `ChatMessage` it is append-only and stored **outside** the rev'd workspace
aggregate (own `task_logs` collection) — posting one does **not** bump `rev` and
never hits a revision conflict. It may optionally reference a resource via
`resource_id` (empty = a workspace-wide note).
```json
{
  "id": "tlg_1a2b3c",
  "author_id": "col_ava",
  "author": "Ava Chen",
  "role": "backend",            // author's role at post time
  "kind": "added",              // added | changed | fixed | removed | note
  "body": "POST /orders now returns the created order body, not just its id.",
  "resource_id": "res_create_order", // optional; "" = workspace-wide note
  "at": "2026-07-10T08:01:00Z"
}
```
- `kind` categorizes the update for badging/filtering; an empty/omitted `kind`
  defaults to `note`. `author`/`author_role`/`at` are derived server-side from the
  authenticated collaborator — never client-submitted.
- A non-empty `resource_id` is validated against the room's resources on create
  (`404 NOT_FOUND` if unknown). A resource deleted **after** the log was posted
  leaves the `resource_id` dangling (kept as-is; not cascade-cleaned).

### ActivityEvent
Append-only audit feed. Emitted by the server on every mutation.
```json
{
  "id": "act_551",
  "actor": "Ava Chen",
  "verb": "added",              // added|edited|removed|cleared|renamed|set draft|set ready|set breaking|flagged|created|commented on
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
  "chat": [ "...ChatMessage" ],   // WS snapshot only — last 200 messages, oldest first
  "task_logs": [ "...TaskLog" ],  // WS snapshot only — last 200 entries, oldest first
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

**Server behavior (backend contract):**
- The server issues the outbound request itself (server-to-server) — this is what
  bypasses the browser's CORS/mixed-content restrictions. Nothing about the caller's
  origin matters.
- `method` / `url` / `headers` / `body` are forwarded as-is. `url` must be absolute
  with an `http`/`https` scheme; anything else → `200` + `status: 0` + `error`.
- **Redirects are NOT auto-followed** — a 3xx is surfaced as-is (more useful when
  validating API behavior). Request **timeout is 20s**; on timeout the transport
  error → `200` + `status: 0` + `error`.
- Response `body` is a UTF-8 string capped at **2 MiB**; `truncated: true` when the
  source was larger. `size` is the returned (capped) byte length.
- `duration_ms` is measured on the server around the outbound call.
- `headers` is the response headers as `{ name: [values...] }` (multi-value safe).
- Any transport/network error (DNS/refused/timeout) is caught and returned in the
  envelope with HTTP `200` + `status: 0` + non-empty `error` — never a 5xx for a
  failed *target*.
- **SSRF guard** (`Executor.AllowPrivate`): when false (prod) requests to
  loopback/link-local/private hosts are rejected; dev defaults to true so it can
  hit `localhost` APIs. See `backend/internal/httpexec/executor.go`.

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

### Story (API Story — persisted)
An ordered business flow that walks readers through the API by **pointing at
existing endpoint Resources** — a Story never duplicates endpoint data, each
endpoint step is just a `resource_id` pointer back to the single source of truth.
Stored in its **own Mongo collection** (`stories`), scoped by `workspace_id` — like
flows, *not* embedded in the rev-guarded workspace document, so authoring a Story
never conflicts with schema edits. **Step order = array order** (persist the array
as-is; no separate `order` field).
```json
{
  "id": "sty_1a2b", "workspace_id": "123456",
  "name": "Checkout flow",
  "steps": [
    { "id": "sst_9f", "type": "endpoint", "resource_id": "res_42", "text": "" },
    { "id": "sst_a0", "type": "section",  "resource_id": "",       "text": "Payment" },
    { "id": "sst_b1", "type": "note",     "resource_id": "",       "text": "retry on 409" }
  ],
  "created_at": "2026-07-07T08:00:00Z", "created_by": "Ava Chen",
  "updated_at": "2026-07-07T08:00:00Z", "updated_by": "Ava Chen"
}
```
- `type`: `endpoint` | `note` | `section`. An `endpoint` step carries `resource_id`
  (pointer to a `Resource`; `text` is an optional annotation). `note`/`section`
  steps carry only `text` and MUST have an empty `resource_id`.
- A dangling `resource_id` (endpoint deleted after the step was added) is kept
  as-is — the UI renders it as *"endpoint no longer exists"*. The backend does **not**
  cascade-clean stories when a resource is deleted.

### Proposal (Proposal Mode — persisted)
A Pull-Request-like review flow for an endpoint's request-body schema: `fields`
is an independent **draft copy** of the target `Resource`'s fields (cloned at
creation), reviewed via threaded `comments` and an append-only `timeline`
before being merged back through the real field endpoints (`/resources/{id}/fields...`).
The `Proposal` document itself is never the source of truth for the published
schema — merging applies the diff through those endpoints, same as any other edit.
Stored in its **own Mongo collection** (`proposals`), scoped by `workspace_id` —
like Story/Flow, *not* embedded in the rev-guarded workspace document, so
drafting a proposal never conflicts with schema edits.
```json
{
  "id": "prp_9c2a", "workspace_id": "123456", "resource_id": "res_42",
  "title": "Add phone field", "description": "Needed for SMS 2FA rollout.",
  "author": "Ava Chen", "author_role": "backend", "status": "reviewing",
  "fields": [ { "id": "fld_1", "key": "phone", "type": "string", "required": false, "state": "draft", "change": "added" } ],
  "comments": [
    { "id": "prc_1", "field_key": "phone", "author": "Bo Lin", "role": "frontend", "body": "should this be E.164?", "resolved": false, "at": "2026-07-08T09:10:00Z" }
  ],
  "timeline": [
    { "id": "ptl_1", "kind": "created", "actor": "Ava Chen", "detail": "created this proposal", "at": "2026-07-08T09:00:00Z" },
    { "id": "ptl_2", "kind": "status", "actor": "Ava Chen", "detail": "requested review", "at": "2026-07-08T09:05:00Z" }
  ],
  "created_at": "2026-07-08T09:00:00Z", "updated_at": "2026-07-08T09:10:00Z", "updated_by": "Bo Lin"
}
```
- `status`: `draft` | `reviewing` | `approved` | `rejected` | `merged`. `author`/
  `author_role` and every `comments[].author`/`role` and `timeline[].actor` are
  derived server-side from the authenticated collaborator — never client-submitted.
- `fields` reuses the `SchemaField` shape (§2 above), including nested `children`/`items`.
- `timeline` is append-only; entries are never edited or removed, only appended
  by the mutating endpoints below.

### Feedback (usage reports — persisted)
A workspace-scoped usage report — a complaint, an improvement request, or a bug —
that any collaborator can file and anyone in the room can move through a simple
status lifecycle. Stored in its **own Mongo collection** (`feedback`), scoped by
`workspace_id` — like Story/Proposal, *not* embedded in the rev-guarded
workspace document.
```json
{
  "id": "fbk_5d43", "workspace_id": "123456",
  "category": "complaint", "body": "The Flows page loads slowly with many runs.",
  "author": "Ava Chen", "author_role": "backend", "status": "open",
  "created_at": "2026-07-13T03:00:00Z", "updated_at": "2026-07-13T03:00:00Z", "updated_by": "Ava Chen"
}
```
- `category`: `complaint` | `improvement` | `bug` | `other` (defaults to `other`).
- `status`: `open` | `in_progress` | `resolved` | `dismissed` (created as `open`).
- `author`/`author_role` are derived server-side from the authenticated
  collaborator — never client-submitted. `updated_by` tracks the last actor
  (e.g. whoever changed the status).

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
| GET | `/resources` | List resources (`?kind=endpoint\|database\|model` and/or `?status=draft\|inprogress\|testing\|done` optional; `status` applies to endpoints only) |
| GET | `/resources/{id}` | One resource (with fields) |
| POST | `/resources` | Create a resource |
| POST | `/resources/import` | Atomically bulk-create imported endpoints; body `{ "endpoints": [ ... ] }`, response `{ "rev", "resources" }` |
| DELETE | `/resources` | **Delete ALL resources in the room**; requires `X-Confirm-Delete-All: true` |
| PATCH | `/resources/{id}` | Rename / set `method` / `path` / endpoint `status` |
| DELETE | `/resources/{id}` | Delete a resource |

### Fields
| method | path | purpose |
|--------|------|---------|
| POST | `/resources/{id}/fields` | Add a single top-level field |
| PATCH | `/resources/{id}/fields/{field_id}` | Update key/type/required/state/description on a single top-level field |
| DELETE | `/resources/{id}/fields/{field_id}` | Remove a single top-level field (see soft-delete rule) |
| PUT | `/resources/{id}/request-fields` | Replace the resource's whole request-body field tree (nested `children`/`items` included); body `{ "fields": [ ...SchemaField ] }`. Backs the Visual Builder's save — see §6. |

### Response schemas
| method | path | purpose |
|--------|------|---------|
| PUT | `/resources/{id}/responses` | Replace the endpoint's per-status response schemas (whole `ResponseSchema[]`, fields may be nested); body `{ "responses": [ ...ResponseSchema ] }` |

### Comments
| method | path | purpose |
|--------|------|---------|
| GET | `/resources/{id}/comments` | Comments for a resource (`?field_id=` to filter) |
| POST | `/resources/{id}/comments` | Add a comment (optionally anchored to a field) |
| DELETE | `/comments/{id}` | Delete a comment (author/admin) |

### Team chat
| method | path | purpose |
|--------|------|---------|
| GET | `/chat` | Last 200 chat messages, oldest first |
| POST | `/chat` | Send a message (`{ "body" }`, max 2000 chars) → `201 { "message": ChatMessage }`; broadcasts `chat.created` |

### Backend work-update log
| method | path | purpose |
|--------|------|---------|
| GET | `/task-logs` | Last 200 task-log entries, oldest first |
| POST | `/task-logs` | Post an update (`{ "kind", "body", "resource_id" }`) → `201 { "task_log": TaskLog }`; broadcasts `task_log.created` |

> Task logs live in a dedicated `task_logs` collection and, like chat, are
> append-only and do **not** bump the workspace `rev` or emit `ActivityEvent`s.
> On `POST`, `body` is required (max 2000 chars); `kind` defaults to `note` when
> empty and must be one of `added | changed | fixed | removed | note`;
> `resource_id` is optional and, when present, must reference an existing resource.
> `author`/`role`/`at` are set server-side from the token.

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

### API Story
| method | path | purpose |
|--------|------|---------|
| POST | `/stories` | First save of a story `{ "name", "steps" }` (scoped to the room) → `Story` |
| GET | `/stories` | List saved stories for the room (newest first) |
| GET | `/stories/{id}` | One story |
| PATCH | `/stories/{id}` | Re-save an existing story `{ "name", "steps" }` (full document) |
| DELETE | `/stories/{id}` | Delete a story |

> Stories live in a dedicated `stories` collection and, like flows, do **not** bump
> the workspace `rev` or emit activity events.
>
> **Edit model = draft then save.** The client builds/edits a story entirely in a
> local in-memory draft (add/move/remove steps, rename, notes/sections) — no
> request per keystroke. A single **Save** flushes the WHOLE story in ONE request:
> `POST /stories` the first time (never-persisted draft), `PATCH /stories/{id}`
> thereafter. Both carry the complete `{ "name", "steps" }`; `steps` is always the
> full ordered array (array order = step order). `PATCH` is therefore a **full
> replace** → last-write-wins per story doc. Every `id` in `steps` is
> client-generated and persisted as-is. `DELETE /stories/{id}` hard-deletes; response `data`:
> `{ "story_id": "sty_1a2b" }`. Peers pick up story changes on their next
> `GET /stories` (on load, or when opening the **API Story** tab); real-time WS push
> is **optional** and not required for v1.

### Proposals
| method | path | purpose |
|--------|------|---------|
| POST | `/proposals` | Create a proposal `{ "resource_id", "title", "description", "fields" }` (fields = draft clone of the target resource's fields) → `Proposal` |
| GET | `/proposals` | List every proposal in the room (newest first); client filters by `resource_id` |
| GET | `/proposals/{id}` | One proposal |
| PATCH | `/proposals/{id}` | Update `{ "title", "description" }` |
| DELETE | `/proposals/{id}` | Delete a proposal |
| POST | `/proposals/{id}/status` | Change status `{ "status": "draft\|reviewing\|approved\|rejected\|merged" }`, appends a `timeline` entry |
| POST | `/proposals/{id}/fields` | Add a single draft field `{ "key", "type", "required", "description" }` |
| PATCH | `/proposals/{id}/fields/{field_id}` | Update a draft field (key/type/required/state/description/value) |
| DELETE | `/proposals/{id}/fields/{field_id}` | Remove a draft field |
| POST | `/proposals/{id}/comments` | Add a comment `{ "field_key", "body" }` (optionally anchored to a field) |
| PATCH | `/proposals/{id}/comments/{comment_id}` | Toggle a comment's `resolved` flag |

> Proposals live in a dedicated `proposals` collection and, like Story/Flow, do
> **not** bump the workspace `rev` or emit WebSocket/activity events — peers pick
> up changes on their next `GET /proposals` (on load, or when opening the
> **Proposals** tab). Every mutating endpoint returns the full updated `Proposal`
> (whole-document replace, last-write-wins). `author`/`author_role` on create and
> `comments[].author`/`role` on comment-add are always derived server-side from
> the authenticated collaborator, never accepted from the request body. Merging a
> proposal is a **client-driven** operation: the UI diffs `proposal.fields`
> against the live `Resource.fields` and applies the changes through the normal
> field endpoints (§3 Fields), then calls `POST /proposals/{id}/status` with
> `"merged"` — there is no dedicated merge endpoint.

### Feedback (usage reports)
| method | path | purpose |
|--------|------|---------|
| POST | `/feedback` | File a report `{ "category?", "body" }` (body required; category defaults to `other`) → `Feedback` (status `open`) |
| GET | `/feedback` | List every report in the room (newest first) |
| POST | `/feedback/{id}/status` | Change status `{ "status": "open\|in_progress\|resolved\|dismissed" }` → updated `Feedback` |
| DELETE | `/feedback/{id}` | Delete a report → `{ "feedback_id" }` |

> Like Proposals, feedback lives in its own collection and does **not** bump the
> workspace `rev` or emit WebSocket/activity events — the Feedback dialog
> re-fetches `GET /feedback` each time it opens.

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
| `resource.cleared` | `{ "rev", "resource_ids": [ "res_…" ] }` (bulk delete-all; clients drop every listed id + its comments) |
| `field.created` / `field.updated` / `field.removed` | `{ "rev", "resource": Resource }` (send the whole updated resource so `state` rollup + fields stay consistent) |
| `resource.updated` (from `PUT /resources/{id}/responses`) | `{ "rev", "resource": Resource }` — reuses the `resource.updated` frame; the resource carries the new `responses` array |
| `comment.created` / `comment.deleted` | `{ "rev", "comment": Comment }` / `{ "rev", "comment_id" }` |
| `activity.created` | `{ "activity": ActivityEvent }` |
| `chat.created` | `{ "message": ChatMessage }` (no `rev` — chat is outside the rev'd aggregate) |
| `task_log.created` | `{ "task_log": TaskLog }` (no `rev` — task logs are outside the rev'd aggregate) |
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
Response `data`: `{ "rev": 43, "resource": { "...Resource" } }`. Endpoint
resources are created with `state:"draft"`, `status:"draft"`, empty `fields:[]`,
and empty `responses:[]`.

> **Seeded `id` field behavior.** A `GET` endpoint sends query params and a
> `POST` endpoint sends a request body, so endpoints must not receive a phantom
> seeded field:
> - `kind:"endpoint"` → create with **no** seeded fields (empty `fields:[]`).
> - `kind:"database"` / `kind:"model"` → keep the seeded `id` (an id column/key
>   is a sensible default).

### PATCH `/resources/{id}`
Request (any subset):
```json
{ "name": "createOrderV2", "path": "/api/v1/v2/orders", "status": "testing" }
```
Response `data`: `{ "rev": 43, "resource": { "...Resource" } }`

> The `path` and `method` are editable for the lifetime of an endpoint — a newly
> created endpoint is seeded with `method:"GET"`, `path:"/api/v1/new"` and the
> client renames/repaths it via this `PATCH`. There is **no** reserved or
> immutable path; `/api/v1/new` is just a placeholder default.
> `status` is valid only for endpoints and must be one of `draft`, `inprogress`,
> `testing`, or `done`.

### DELETE `/resources/{id}`
Permanently removes a resource (endpoint / database / model) and all of its
fields. Hard delete — the resource is gone, not soft-flagged (unlike a field's
soft-delete rule). Emits a `resource.deleted` over WebSocket (§4) and an
`ActivityEvent` (`verb:"removed"`).

Response `data`: `{ "rev": 45, "resource_id": "res_create_order" }`

### DELETE `/resources`
Wipes **every** resource in the room (and their comments) in one rev-bumping
mutation. This is an explicit destructive action and must include
`X-Confirm-Delete-All: true`; otherwise the server returns
`422 VALIDATION_ERROR` and leaves the workspace unchanged. Spec import must use
`POST /resources/import` directly, not a delete-then-create sequence. A no-op on
an empty workspace returns the current `rev` without bumping or broadcasting.
Emits `resource.cleared` (§4) and one `ActivityEvent` (`verb:"cleared"`,
`target:"all resources"`).

Response `data`: `{ "rev": 46, "resource_ids": [ "res_user", "res_create_order" ] }`

### POST `/resources/{id}/fields`
Request:
```json
{ "key": "couponCode", "type": "string", "required": false }
```
- `state` defaults to `draft`, `change` to `added`.
- `422 VALIDATION_ERROR` if `key` already exists on the resource.

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

### PUT `/resources/{id}/responses`
Replaces the endpoint's per-status response schemas with the posted array (whole-array
semantics — the client sends the full desired state). Request:
```json
{
  "responses": [
    { "status": 200, "description": "OK",
      "fields": [ { "id": "fld_id", "key": "id", "type": "uuid", "required": true,
                    "state": "ready", "change": "added", "description": null, "value": null } ] },
    { "status": 404, "description": "Not Found", "fields": [] }
  ]
}
```
- Endpoints only; bumps `rev`, sets `updated_at`/`updated_by`, appends an
  `ActivityEvent` (e.g. `verb:"edited"`, `target:"responses"`), and broadcasts
  `resource.updated` over the WebSocket.
- Response `data`: `{ "rev", "resource": { "...Resource" } }` (full resource,
  including the stored `responses`).

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

### POST `/http/test`
```json
// request body
{ "method": "GET", "url": "https://api.example.com/users?limit=5",
  "headers": { "Authorization": "Bearer xyz" }, "body": "" }
// success (target answered)
{ "success": true, "message": "OK",
  "data": { "status": 200, "duration_ms": 118,
            "headers": { "Content-Type": ["application/json"] },
            "body": "[{...}]", "size": 842, "truncated": false, "error": "" } }
// target unreachable — still HTTP 200 (handler reports duration_ms: 0 on error)
{ "success": true, "message": "OK",
  "data": { "status": 0, "duration_ms": 0, "headers": {},
            "body": "", "size": 0, "error": "context deadline exceeded" } }
```

---

## 6. Frontend rendering contract (what the UI needs)

| UI element | source field(s) |
|------------|-----------------|
| Left explorer groups | `Resource.kind` (`endpoint` / `database` / `model`) |
| Endpoint method tag + path | `Resource.method`, `Resource.path` |
| Resource state dot / badge | `Resource.state` (server rollup) |
| Endpoint status pill (Draft / In Progress / Testing / Done) | `Resource.status` (endpoints only, see §2) |
| Explorer status filter chips (All / Draft / In Progress / Testing / Done) | `Resource.status`; backend filter `GET /resources?status=` |
| Blueprint field row | `SchemaField` (`key`, `type`, `required`, `description`) |
| `[Draft] [Ready] [Breaking Change]` badge | `SchemaField.state` |
| Diff line-weight / colour | `SchemaField.change` |
| Field comment count | `Comment[]` where `resource_id` + `field_id` match |
| "updated 2m ago by X" | `Resource.updated_at`, `updated_by` |
| Request body tabs (Visual / JSON Schema / Example JSON / Example TypeScript) | `Resource.fields` (nested), synced via `PUT /resources/{id}/request-fields` (write-through debounced save, `schemaTreeSync.ts`) |
| Response tabs (per HTTP status) | `Resource.responses` (`ResponseSchema[]`, nested fields, see §2), synced via `PUT /resources/{id}/responses` |
| Explorer "Bookmarked" group (pinned top) | client-side bookmark set (frontend-local, see §2) |
| Right · Activity tab | `ActivityEvent[]` (newest first) |
| Right · Comments tab | `Comment[]` (filtered by selected resource / focused field) |
| Top bar presence avatars | `Collaborator[]` + live `Presence` (online if heartbeat within TTL) |
| Top bar "Import API" | **`POST /resources/import`** with selected endpoints; do not call `DELETE /resources` first |

The client treats each read/WS payload as the **single source of truth**, merges
mutations by `rev` (last-writer-wins on conflict, with `409` rebase), and never
authors `state` rollups, `updated_at`, or `activity` — those are server-owned.
```
