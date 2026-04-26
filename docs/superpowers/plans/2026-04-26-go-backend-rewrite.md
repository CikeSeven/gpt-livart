# Go Backend Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go backend that can replace the original livart Spring Boot backend for the unchanged React frontend.

**Architecture:** Implement a single Go binary with route handlers separated from persistence and object storage. Keep route names, response envelopes, JSON fields, and WebSocket messages compatible with the source project.

**Tech Stack:** Go 1.26, `chi`, `pgx`, `bcrypt`, `jwt/v5`, `gorilla/websocket`, MinIO Go SDK, standard library tests.

---

### Task 1: Scaffold Tests And Module

**Files:**
- Create: `go.mod`
- Create: `internal/api/api_test.go`

- [x] **Step 1: Write failing tests**

The tests exercise the desired backend behavior before implementation exists.

- [x] **Step 2: Run tests to verify failure**

Run: `go test ./...`
Expected: FAIL because `NewTestServer` and related API implementation do not exist.

### Task 2: Implement Core API Server

**Files:**
- Create: `internal/api/server.go`
- Create: `internal/api/memory.go`

- [x] **Step 1: Add common response helpers and auth middleware**
- [x] **Step 2: Implement auth, user config, canvas, image reference, image job, and export handlers against interfaces**
- [x] **Step 3: Run tests until green**

### Task 3: Implement Production Wiring

**Files:**
- Create: `cmd/livart/main.go`
- Create: `internal/config/config.go`
- Create: `internal/app/app.go`
- Create: `internal/store/postgres.go`
- Create: `internal/assets/storage.go`

- [x] **Step 1: Add environment config compatible with original Docker variables**
- [x] **Step 2: Add PostgreSQL migrations for original `artisan_*` tables**
- [x] **Step 3: Add local and MinIO object stores**
- [x] **Step 4: Wire the HTTP server binary**
- [x] **Step 5: Run `go test ./...` and `go build ./cmd/livart`**

### Task 4: Deployment Files And Documentation

**Files:**
- Create: `README.md`
- Create: `.env.example`
- Create: `Dockerfile`
- Create: `docker-compose.yml`

- [x] **Step 1: Document local and Docker startup**
- [x] **Step 2: Provide environment example matching original names**
- [x] **Step 3: Provide Docker build and compose files for Go backend with PostgreSQL and MinIO**
- [x] **Step 4: Run final verification**

## Self-Review

- Spec coverage: backend-only rewrite, compatible routes, envelope, auth, user config, canvas, asset, AI job, WebSocket, and export behavior are covered.
- Placeholder scan: no TBD/TODO placeholders remain in the plan.
- Type consistency: route names and JSON fields match the source frontend expectations.
