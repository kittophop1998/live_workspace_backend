# MCP interface

The backend can expose an authenticated, HTTP-based Model Context Protocol
(MCP) endpoint alongside the existing REST API. It is disabled by default.

## Enable it

Set:

```env
MCP_ENABLED=true
MCP_PATH=/mcp
```

Restart the API process. The REST routes are unchanged. When MCP is disabled,
the MCP route is not registered.

## Authentication

Send the same JWT used by the REST API:

```http
Authorization: Bearer <access-token>
```

The token's `workspace_id` claim selects the workspace, and the user (`sub`)
must still be a collaborator in that workspace. Tool calls cannot select
another workspace. Tokens, secrets, and tool arguments are not written to MCP
request logs.

## Tools

The initial interface is read-only:

- `listProjects`
- `getProject`
- `listEndpoints`
- `getEndpoint`
- `getOpenAPISpec`
- `getJSONSchema`
- `getWorkflow`
- `listComments`

This application currently treats its Live Workspace as the project boundary.
Consequently, `project_id` is the authenticated workspace ID. OpenAPI and JSON
Schema results are generated from current endpoint/resource data; they are not
independently persisted specifications.

## Client configuration

For an MCP client that supports remote HTTP servers, configure:

```json
{
  "mcpServers": {
    "fark-noi": {
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer <access-token>"
      }
    }
  }
}
```

The endpoint accepts MCP JSON-RPC over HTTP `POST` and supports `initialize`,
`ping`, `tools/list`, and `tools/call`.

## Safety and limitations

- No delete, update, workflow execution, database-write, or other production
  mutation tools are exposed.
- Each request is authenticated independently; the server is stateless and
  does not expose a stdio transport.
- Tool results reflect only the workspace authorized by the JWT and current
  collaborator membership.
- Generated OpenAPI responses describe the shared resource fields. They do not
  infer parameters, authentication schemes, servers, or error responses that
  are not represented in the existing workspace model.
