# MongoDB structure

Workspace data is split across collections. The domain and use-case layers still
treat a workspace as one aggregate.

## Collections

| Collection | Purpose | Important fields |
| --- | --- | --- |
| `workspaces` | Published aggregate revision | `_id`, `rev` |
| `resources` | API, model, and database resources | `workspace_id`, `workspace_rev`, `position`, `kind` |
| `comments` | Resource and field comments | `workspace_id`, `workspace_rev`, `position`, `resource_id` |
| `activities` | Workspace activity history | `workspace_id`, `workspace_rev`, `position`, `at` |
| `collaborators` | Room members | `workspace_id`, `workspace_rev`, `position` |
| `flows` | Persisted E2E flow definitions | `workspace_id`, `created_at` |
| `flow_runs` | Flow execution history | `workspace_id`, `flow_id`, `started_at` |
| `chat_messages` | Project-wide team chat (append-only, outside the rev'd aggregate) | `workspace_id`, `at` |
| `task_logs` | Backend work-update log (append-only, outside the rev'd aggregate) | `workspace_id`, `kind`, `resource_id`, `at` |

Child records use readable `snake_case` BSON field names. `_id` has the form
`<workspace_id>:<workspace_rev>:<entity_id>`.

## Revision consistency

A save writes all child records under the next `workspace_rev`, then atomically
changes `workspaces.rev` with a compare-and-swap filter. Reads only load child
records for the published revision. This preserves optimistic concurrency
without requiring MongoDB transactions or a replica set.

Old child revisions are deleted after a successful publish. `position` preserves
the original array ordering when records are loaded from separate collections.

## Legacy migration

At startup, `MigrateLegacy` finds old `workspaces` documents containing embedded
`resources`, `comments`, `activity`, or `collaborators`. It upserts those records
into the split collections and then removes the embedded fields. The operation
is idempotent and can safely resume after interruption.
