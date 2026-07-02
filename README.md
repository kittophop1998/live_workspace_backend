# Fark Noi Backend

Go/Gin API backed by MongoDB for the collaboration contract in `api-spec.md`.

## Run locally

```bash
cp .env.example .env
set -a; source .env; set +a
go run ./cmd/api
```

The API is available at `http://localhost:8080/api/v1`.

## Run backend in Docker with external MongoDB

```bash
cp .env.compose.example .env
# Edit MONGO_URI in .env to point to the external MongoDB instance.
docker compose up -d --build
```

The API is available at `http://localhost:8081/api/v1` by default. Change
`API_PORT` to expose a different host port. For MongoDB running on the Docker
host, use `mongodb://host.docker.internal:27017`; for a remote deployment, use
its normal `mongodb://` or `mongodb+srv://` connection string.

## MCP

Backend มี HTTP MCP endpoint แบบ authenticated และ read-only ซึ่งปิดไว้เป็น
ค่าเริ่มต้น ดูวิธีเปิดใช้งาน ขอ token ตั้งค่า client และเรียก tools ได้ที่
[คู่มือใช้งาน MCP](docs/mcp.md)

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
