# gpt-livart

gpt-livart 是一个面向 AI 图像创作工作台的 Go 后端项目，提供账号登录、用户 API 配置、永久画布、图片资源管理、AI 图像接口代理、异步生图状态、WebSocket 推送和交付包导出等能力。

项目目标是为无限画布式 AI 创作体验提供一个轻量、可部署、易维护的服务端基础。前端可以围绕画布组织图片、提示词、局部编辑和派生结果，后端负责保存项目状态、管理图片资产、隔离用户配置，并把图像生成请求代理到 OpenAI Images API 兼容服务。

## 功能特性

- **账号体系**：支持注册、登录、JWT 鉴权和当前用户信息读取。
- **用户配置**：按用户保存 AI 网关 Base URL、API Key、生图模型和对话模型。
- **永久画布**：支持多项目画布、画布状态保存、revision 防旧数据覆盖和快照记录。
- **图片资产**：支持图片上传、资源元数据保存、原图读取、预览图和缩略图 URL 契约。
- **AI 代理**：提供文生图、图生图和局部编辑接口代理，隐藏用户 API Key 的直接暴露。
- **生图任务**：提供 image job 提交、状态读取和 WebSocket 状态推送接口。
- **图片引用分析**：为 `@图片` 多图编辑场景提供基础角色分析能力。
- **导出交付**：支持把画布图片资源打包为 ZIP 文件下载。
- **Docker 部署**：提供 PostgreSQL、MinIO 和 Go 服务的 compose 示例。

## 技术栈

- Go
- PostgreSQL
- MinIO / S3-compatible object storage
- JWT
- WebSocket
- Docker Compose

## 项目结构

```text
cmd/livart/        应用入口
internal/api/      HTTP API、鉴权中间件、WebSocket、测试内存实现
internal/app/      服务依赖组装
internal/assets/   本地文件和 MinIO 对象存储实现
internal/config/   环境变量配置加载
internal/store/    PostgreSQL 持久化和表结构初始化
docs/              设计与实现计划文档
```

## 快速开始

### 本地运行

先准备 PostgreSQL，然后执行：

```bash
cp .env.example .env
set -a
source .env
set +a
go run ./cmd/livart
```

服务默认监听 `8080` 端口，可通过 `SERVER_PORT` 或 `LIVART_PORT` 修改。

### Docker Compose

```bash
docker compose up -d --build
```

Compose 会启动：

- `postgres`：保存用户、画布、快照和资源元数据。
- `minio`：保存上传图片和生成图片资源。
- `livart-go`：Go 后端服务。

## 环境变量

常用配置见 `.env.example`，主要包括：

- `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD`
- `MINIO_ENDPOINT` / `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY` / `MINIO_BUCKET`
- `JWT_SECRET` / `JWT_TTL_DAYS`
- `LIVART_DEFAULT_API_BASE_URL` / `LIVART_DEFAULT_API_KEY`
- `LIVART_DEFAULT_IMAGE_MODEL` / `LIVART_DEFAULT_CHAT_MODEL`

如果配置了 `LIVART_DEFAULT_API_BASE_URL` 和 `LIVART_DEFAULT_API_KEY`，新用户可以直接使用服务端默认 AI 网关；否则用户需要在页面中保存自己的 API 配置。

## 接口概览

- `POST /api/auth/register`：注册账号。
- `POST /api/auth/login`：登录账号。
- `GET /api/auth/me`：读取当前用户。
- `GET /api/user/config`：读取用户 AI 配置。
- `PUT /api/user/config`：保存用户 AI 配置。
- `GET /api/canvases`：读取画布项目列表。
- `POST /api/canvases`：创建画布项目。
- `GET /api/canvases/{id}`：读取画布项目。
- `PUT /api/canvases/{id}`：保存画布项目。
- `POST /api/assets`：上传图片资源。
- `POST /api/images/generations`：代理文生图请求。
- `POST /api/images/edits`：代理图生图或局部编辑请求。
- `POST /api/image-jobs/generations`：提交异步文生图任务。
- `POST /api/image-jobs/edits`：提交异步图生图任务。
- `WS /ws/image-jobs`：订阅生图任务状态。
- `POST /api/exports/images`：创建图片 ZIP 导出。

## 验证

```bash
go test ./...
go build ./cmd/livart
```

## 参考项目

gpt-livart 的产品方向、画布工作流和 AI 图像创作体验参考并学习了以下开源项目：

- [EaseeSoft/ArtisanLab](https://github.com/EaseeSoft/ArtisanLab)
- [yiersan-2026/livart](https://github.com/yiersan-2026/livart)
