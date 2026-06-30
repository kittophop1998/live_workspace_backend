# AGENTS.md

Guidelines for AI/code agents working in this repository.

## Project

- Backend for Fark Noi.
- Language: Go.
- HTTP framework: Gin.
- Database: MongoDB.
- Architecture: Clean Architecture with dependency injection.

## Non-Negotiable Rules

- Keep Clean Architecture boundaries intact.
- Do not make `internal/domain` depend on Gin, MongoDB, S3, JWT libraries, environment variables, or HTTP response shapes.
- Do not make `internal/usecase` depend on Gin handlers, Mongo collections, BSON filters, or concrete adapter implementations.
- All external I/O must live in adapters or infrastructure packages.
- `cmd/api` is the composition root. Wire config, repositories, clients, use cases, handlers, router, seeding, and shutdown there.
- Prefer small, boring Go code over clever abstractions.
- Run `gofmt` on edited Go files before finishing.

## Current Package Boundaries

- `cmd/api`: application entrypoint and dependency wiring only.
- `internal/config`: environment loading and typed configuration.
- `internal/domain/entity`: business entities, value objects, enums, and domain constants.
- `internal/domain/port`: interfaces required by use cases, especially repositories.
- `internal/usecase`: application/business flows. Depends on domain entities and ports.
- `internal/adapter/http`: Gin router, middleware, request DTOs, response DTOs, and handlers.
- `internal/adapter/repository/mongo`: MongoDB repository implementations and index setup.
- `internal/seed`: startup seed logic.
- `pkg`: reusable support packages that are not domain-specific.

## Dependency Direction

Allowed direction:

```text
cmd/api
  -> internal/adapter/http
  -> internal/adapter/repository/mongo
  -> internal/usecase
  -> internal/domain
```

Core rule:

```text
domain <- usecase <- adapter <- cmd
```

Imports must point inward. Inner layers must not import outer layers.

## Domain Layer Rules

- Domain entities must express business meaning only.
- Domain code should not call databases, HTTP clients, object storage, or environment variables.
- Domain structs should not gain new `bson` tags. Existing tags are legacy coupling; when touching a repository/entity pair, migrate persistence mapping into the Mongo adapter instead of spreading more tags.
- Avoid returning Gin-ready response maps from domain or usecase code.
- Put domain errors or validation rules close to the domain/usecase that owns them.
- Use explicit enums for statuses and roles instead of raw strings in business logic.

## Use Case Rules

- Use cases receive dependencies through interfaces from `internal/domain/port`.
- Use cases should accept `context.Context` as the first parameter for operations that may do I/O.
- Use cases should own business decisions: state transitions, authorization decisions, fee calculations, payment flow decisions, and validation that is not purely HTTP syntax.
- Use cases should return domain entities or usecase result structs, not Gin contexts or Mongo documents.
- Wrap meaningful errors with context where it helps callers debug.
- Do not ignore repository errors unless there is a deliberate fallback and the reason is obvious in code.

## HTTP Adapter Rules

- Gin code stays in `internal/adapter/http`.
- Handlers parse route params, query params, auth context, and JSON bodies.
- Handlers call one use case method per user action when possible.
- Request DTOs and response DTOs belong in handler packages, not in domain.
- Use `ShouldBindJSON` with validation tags for request validation.
- Keep route registration in `router.go`; group routes by API version and feature.
- Convert usecase errors to stable HTTP status codes and response bodies at the handler boundary.
- Keep Swagger documentation current whenever adding, removing, or changing an API route, request shape, response shape, auth requirement, query/path parameter, or status code.
- After changing Swagger annotations, regenerate docs with `swag init -g cmd/api/main.go -o internal/adapter/http/docs --parseInternal` and commit the generated docs.

## Mongo Adapter Rules

- Mongo code stays in `internal/adapter/repository/mongo`.
- Repositories implement interfaces from `internal/domain/port`.
- Collection names, BSON field names, filters, update documents, indexes, and transactions belong in this adapter only.
- Prefer atomic Mongo updates (`$set`, `$inc`, `$push`, `$pull`) over read-modify-write for concurrent state changes.
- Add indexes in `EnsureIndexes` when adding a query path that filters or sorts on new fields.
- Return `nil, nil` for not-found lookups only if the repository interface already follows that convention.
- Prefer soft delete/status transitions for business records unless hard delete is explicitly required.

## Configuration And Secrets

- Read environment variables only in `internal/config`.
- Do not hardcode production secrets.
- Defaults may be used for local development only.
- If a new environment variable is added, update `.env.example` and the `Config` struct together.

## Testing Expectations

- Add focused tests for new business rules in `internal/usecase`.
- Add handler tests with `httptest` for new HTTP behavior when practical.
- Mock or fake repository ports in usecase tests.
- Do not require a live MongoDB for ordinary unit tests.
- Before finishing a code change, run at least:

```bash
go test ./...
```

## Go Style

- Use short, lowercase package names.
- Keep interfaces small and define them on the consumer side.
- Prefer constructor functions for structs with dependencies.
- Avoid package-level mutable state.
- Do not use panic for normal control flow.
- Do not swallow errors with `_` unless the operation is explicitly best-effort.
- Keep comments useful and ASCII unless the file already intentionally uses another encoding.

## Adding A New Feature

1. Add or update domain entities/enums in `internal/domain/entity`.
2. Add or update ports in `internal/domain/port` only for dependencies the use case needs.
3. Implement business flow in `internal/usecase`.
4. Implement persistence in `internal/adapter/repository/mongo`.
5. Add HTTP DTOs and handlers in `internal/adapter/http/handler`.
6. Register routes in `internal/adapter/http/router.go`.
7. Wire dependencies in `cmd/api/bootstrap.go`.
8. Add or update Swagger annotations and regenerate `internal/adapter/http/docs`.
9. Add or update tests.

## Known Architecture Debt

- Several existing domain entities still contain `json` and `bson` tags and are decoded directly by Mongo repositories. Do not copy this pattern into new code.
- When modifying an existing entity/repository area, prefer a small migration toward adapter-owned DTO/PO mapping.
- Keep each cleanup scoped to the feature being changed; avoid broad unrelated rewrites.
