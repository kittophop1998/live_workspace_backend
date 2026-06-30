# Fark Noi Backend

Go/Gin API backed by MongoDB for the collaboration contract in `api-spec.md`.

## Run locally

```bash
cp .env.example .env
docker compose up -d mongo
set -a; source .env; set +a
go run ./cmd/api
```

The API is available at `http://localhost:8080/api/v1`.

Create a permanent room and receive its full session plus an access token:

```bash
curl -X POST http://localhost:8080/api/v1/rooms \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice"}'
```

Join with the six-digit room code. Logging in again with the same name restores
the same collaborator identity and returns the room's complete persisted session:

```bash
curl -X POST http://localhost:8080/api/v1/rooms/join \
  -H 'Content-Type: application/json' \
  -d '{"room_code":"123456","name":"Bob"}'
```

## Verify

```bash
go test ./...
```
