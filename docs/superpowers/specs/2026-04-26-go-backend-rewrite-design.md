# Go Backend Rewrite Design

## Goal

Rebuild the existing livart Spring Boot backend in Go while keeping the React/Vite frontend contract stable. The new service must expose the same primary API routes, JSON response shape, WebSocket message shape, environment variable names, and PostgreSQL table semantics used by `/home/sisct/Code/oss/livart`.

## Scope

The rewrite covers the backend only. The frontend remains the original React/Vite app and should be able to call the Go backend through the same `/api/*` and `/ws/image-jobs` paths.

## Architecture

The Go service is a single binary with focused internal packages:

- `cmd/livart`: process entrypoint.
- `internal/config`: environment-backed configuration.
- `internal/app`: dependency wiring and HTTP server construction.
- `internal/api`: route handlers, auth middleware, common response helpers, WebSocket handling, image jobs, and export endpoints.
- `internal/store`: PostgreSQL schema migration and persistence.
- `internal/assets`: object storage abstraction with local filesystem and MinIO implementations.

The service uses PostgreSQL as the source of truth for users, API configs, canvases, snapshots, and asset metadata. Asset bytes are stored through an object storage interface so local development works without MinIO while production can use MinIO-compatible storage.

## Compatibility

The service keeps these contracts compatible with the source project:

- Unified API envelope: `{ "success": boolean, "data": any, "error": { "message": string, "code": string } }`.
- Auth routes: `/api/auth/register`, `/api/auth/login`, `/api/auth/me`, `/api/auth/logout`.
- User config routes: `/api/user/config`.
- Canvas routes: `/api/canvases`, `/api/canvases/{id}`, `/api/canvas/current`.
- Asset routes: `/api/assets`, `/api/assets/{id}/content`, `/preview`, `/thumbnail`, `/view/{width}`.
- AI routes: `/api/images/generations`, `/api/images/edits`, `/api/image-jobs/generations`, `/api/image-jobs/edits`, `/api/image-jobs/{jobId}`, `/api/image-references/analyze`.
- WebSocket route: `/ws/image-jobs` with `connected`, `authenticated`, `pong`, `image-job`, and `image-job-error` messages.
- Export routes: `/api/exports/images`, `/api/exports/{exportId}/download`.

## Deliberate Differences

The first Go version performs canvas saves through an in-process ordered queue instead of requiring RabbitMQ. This keeps the frontend behavior stable while reducing bootstrap risk. RabbitMQ configuration can be wired later behind the same queue interface without changing handlers.

Image preview, thumbnail, and canvas view endpoints fall back to the original image bytes. The URL contract remains stable, so WebP tier generation can be added later without frontend changes.

Prompt optimization is represented by preserving original and optimized prompt metadata. The first implementation treats the optimized prompt as the original prompt unless a future optimizer is configured.

## Testing

Unit tests cover the API envelope, auth flow, canvas flow, user config fallback, image job WebSocket-compatible job state, and image reference analysis behavior using in-memory dependencies. Build verification covers all packages.
