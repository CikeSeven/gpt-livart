# gpt-livart

Go backend rewrite for `livart`, keeping the original React/Vite frontend API contract stable.

## Scope

This repository rewrites only the backend from `/home/sisct/Code/oss/livart/backend`. The frontend can continue to call the same paths:

- `/api/auth/*`
- `/api/user/config`
- `/api/canvases/*`
- `/api/canvas/current`
- `/api/assets/*`
- `/api/images/*`
- `/api/image-jobs/*`
- `/ws/image-jobs`
- `/api/exports/*`

## Current Compatibility

- JWT registration, login, current user, and logout.
- User AI gateway configuration with server default fallback.
- Project canvas list, create, load, and revision-based save.
- Asset upload and original-byte fallback for content, preview, thumbnail, and width-tier view routes.
- AI image sync proxy routes and image job routes with livart prompt metadata headers.
- WebSocket image job messages compatible with the frontend message shape.
- Image reference analysis local fallback.
- ZIP export creation and authenticated download.
- PostgreSQL schema compatible with the original `artisan_*` tables.
- MinIO-compatible object storage when configured, local filesystem object storage otherwise.

## Local Development

Start PostgreSQL, then run:

```bash
cp .env.example .env
set -a
source .env
set +a
go run ./cmd/livart
```

The server listens on `SERVER_PORT` or `LIVART_PORT`, defaulting to `8080`.

## Docker

```bash
docker compose up -d --build
```

The compose file starts PostgreSQL, MinIO, and the Go backend. RabbitMQ variables are intentionally accepted for compatibility with the original project, but the current Go backend uses an in-process ordered save/job path rather than requiring RabbitMQ.

## Verification

```bash
go test ./...
go build ./cmd/livart
```

## Notes

The first Go version preserves URL and payload contracts. Preview generation currently falls back to the original image bytes, so `/preview`, `/thumbnail`, and `/view/{width}` work without WebP derivative generation. A later storage enhancement can add WebP tiers behind the same routes without frontend changes.
