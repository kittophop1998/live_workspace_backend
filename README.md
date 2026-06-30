# Fark Noi Backend

Go/Gin API backed by MongoDB for the collaboration contract in `api-spec.md`.

## Run locally

```bash
cp .env.example .env
docker compose up -d mongo
set -a; source .env; set +a
go run ./cmd/api
```

The API is available at `http://localhost:8080/api/v1`. When `JWT_SECRET` is
empty, requests without a token use `DEV_COLLABORATOR_ID`.

## Verify

```bash
go test ./...
```
