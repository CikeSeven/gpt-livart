package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gpt-livart/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool    *pgxpool.Pool
	exports map[string]api.ExportFile
	mu      sync.RWMutex
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	store := &PostgresStore{pool: pool, exports: map[string]api.ExportFile{}}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS artisan_users (
    id UUID PRIMARY KEY,
    username VARCHAR(80) NOT NULL UNIQUE,
    display_name VARCHAR(120) NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    password_hash VARCHAR(120) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE artisan_users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE artisan_users
SET is_admin = TRUE
WHERE id = (
  SELECT id FROM artisan_users ORDER BY created_at ASC LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM artisan_users WHERE is_admin = TRUE);
CREATE TABLE IF NOT EXISTS artisan_canvases (
    id UUID PRIMARY KEY,
    title VARCHAR(200) NOT NULL DEFAULT '默认画布',
    state_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revision BIGINT NOT NULL DEFAULT 0,
    user_id UUID REFERENCES artisan_users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS artisan_canvas_snapshots (
    id UUID PRIMARY KEY,
    canvas_id UUID NOT NULL REFERENCES artisan_canvases(id) ON DELETE CASCADE,
    state_json JSONB NOT NULL,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_artisan_canvas_snapshots_canvas_created ON artisan_canvas_snapshots(canvas_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_artisan_canvases_user_updated ON artisan_canvases(user_id, updated_at DESC, created_at DESC);
CREATE TABLE IF NOT EXISTS artisan_assets (
    id UUID PRIMARY KEY,
    canvas_id UUID REFERENCES artisan_canvases(id) ON DELETE SET NULL,
    user_id UUID REFERENCES artisan_users(id) ON DELETE SET NULL,
    object_key VARCHAR(512) NOT NULL UNIQUE,
    url_path VARCHAR(512) NOT NULL,
    original_filename VARCHAR(255),
    mime_type VARCHAR(120) NOT NULL,
    size_bytes BIGINT NOT NULL,
    width INTEGER,
    height INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_artisan_assets_canvas_created ON artisan_assets(canvas_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_artisan_assets_user_created ON artisan_assets(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS artisan_user_api_configs (
    user_id UUID PRIMARY KEY REFERENCES artisan_users(id) ON DELETE CASCADE,
    base_url VARCHAR(500) NOT NULL,
    api_key TEXT NOT NULL,
    image_model VARCHAR(120) NOT NULL,
    chat_model VARCHAR(120) NOT NULL,
    image_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    chat_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE artisan_user_api_configs ADD COLUMN IF NOT EXISTS image_models JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE artisan_user_api_configs ADD COLUMN IF NOT EXISTS chat_models JSONB NOT NULL DEFAULT '[]'::jsonb;
CREATE TABLE IF NOT EXISTS artisan_system_api_config (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    base_url VARCHAR(500) NOT NULL,
    api_key TEXT NOT NULL,
    image_model VARCHAR(120) NOT NULL,
    chat_model VARCHAR(120) NOT NULL,
    image_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    chat_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT artisan_system_api_config_singleton CHECK (id = 1)
);
ALTER TABLE artisan_system_api_config ADD COLUMN IF NOT EXISTS image_models JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE artisan_system_api_config ADD COLUMN IF NOT EXISTS chat_models JSONB NOT NULL DEFAULT '[]'::jsonb;`)
	return err
}

func (s *PostgresStore) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM artisan_users`).Scan(&count)
	return count, err
}

func (s *PostgresStore) CreateUser(ctx context.Context, user api.UserRecord) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO artisan_users (id, username, display_name, is_admin, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`, user.ID, user.Username, user.DisplayName, user.IsAdmin, user.PasswordHash, user.CreatedAt)
	return err
}

func (s *PostgresStore) FindUserByUsername(ctx context.Context, username string) (api.UserRecord, error) {
	return s.readUser(ctx, `SELECT id::text, username, display_name, is_admin, password_hash, created_at FROM artisan_users WHERE username=$1`, username)
}

func (s *PostgresStore) FindUserByID(ctx context.Context, id string) (api.UserRecord, error) {
	return s.readUser(ctx, `SELECT id::text, username, display_name, is_admin, password_hash, created_at FROM artisan_users WHERE id=$1`, id)
}

func (s *PostgresStore) readUser(ctx context.Context, sql string, args ...any) (api.UserRecord, error) {
	var user api.UserRecord
	err := s.pool.QueryRow(ctx, sql, args...).Scan(&user.ID, &user.Username, &user.DisplayName, &user.IsAdmin, &user.PasswordHash, &user.CreatedAt)
	return user, err
}

func (s *PostgresStore) GetAPIConfig(ctx context.Context, userID string) (api.APIConfigResponse, bool, error) {
	var config api.APIConfigResponse
	var imageModelsJSON, chatModelsJSON string
	err := s.pool.QueryRow(ctx, `SELECT base_url, api_key, image_model, chat_model, COALESCE(image_models, '[]'::jsonb)::text, COALESCE(chat_models, '[]'::jsonb)::text, updated_at FROM artisan_user_api_configs WHERE user_id=$1`, userID).Scan(&config.BaseURL, &config.APIKey, &config.Model, &config.ChatModel, &imageModelsJSON, &chatModelsJSON, &config.UpdatedAt)
	if err == pgx.ErrNoRows {
		return config, false, nil
	}
	if err != nil {
		return config, false, err
	}
	config.ImageModels = decodeModelList(imageModelsJSON, config.Model)
	config.ChatModels = decodeModelList(chatModelsJSON, config.ChatModel)
	config.TextToImageURL = joinURL(config.BaseURL, "images/generations")
	config.ImageToImageURL = joinURL(config.BaseURL, "images/edits")
	return config, true, nil
}

func (s *PostgresStore) SaveAPIConfig(ctx context.Context, userID string, config api.APIConfigResponse) (api.APIConfigResponse, error) {
	config.UpdatedAt = time.Now().UTC()
	err := s.pool.QueryRow(ctx, `INSERT INTO artisan_user_api_configs (user_id, base_url, api_key, image_model, chat_model, image_models, chat_models, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (user_id) DO UPDATE SET base_url=EXCLUDED.base_url, api_key=EXCLUDED.api_key, image_model=EXCLUDED.image_model, chat_model=EXCLUDED.chat_model, image_models=EXCLUDED.image_models, chat_models=EXCLUDED.chat_models, updated_at=EXCLUDED.updated_at RETURNING updated_at`, userID, config.BaseURL, config.APIKey, config.Model, config.ChatModel, encodeModelList(config.ImageModels), encodeModelList(config.ChatModels), config.UpdatedAt).Scan(&config.UpdatedAt)
	return config, err
}

func (s *PostgresStore) GetSystemAPIConfig(ctx context.Context) (api.APIConfigResponse, bool, error) {
	var config api.APIConfigResponse
	var imageModelsJSON, chatModelsJSON string
	err := s.pool.QueryRow(ctx, `SELECT base_url, api_key, image_model, chat_model, COALESCE(image_models, '[]'::jsonb)::text, COALESCE(chat_models, '[]'::jsonb)::text, updated_at FROM artisan_system_api_config WHERE id=1`).Scan(&config.BaseURL, &config.APIKey, &config.Model, &config.ChatModel, &imageModelsJSON, &chatModelsJSON, &config.UpdatedAt)
	if err == pgx.ErrNoRows {
		return config, false, nil
	}
	if err != nil {
		return config, false, err
	}
	config.ImageModels = decodeModelList(imageModelsJSON, config.Model)
	config.ChatModels = decodeModelList(chatModelsJSON, config.ChatModel)
	config.TextToImageURL = joinURL(config.BaseURL, "images/generations")
	config.ImageToImageURL = joinURL(config.BaseURL, "images/edits")
	config.ServerDefault = true
	return config, true, nil
}

func (s *PostgresStore) SaveSystemAPIConfig(ctx context.Context, config api.APIConfigResponse) (api.APIConfigResponse, error) {
	config.UpdatedAt = time.Now().UTC()
	config.ServerDefault = true
	err := s.pool.QueryRow(ctx, `INSERT INTO artisan_system_api_config (id, base_url, api_key, image_model, chat_model, image_models, chat_models, updated_at) VALUES (1, $1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO UPDATE SET base_url=EXCLUDED.base_url, api_key=EXCLUDED.api_key, image_model=EXCLUDED.image_model, chat_model=EXCLUDED.chat_model, image_models=EXCLUDED.image_models, chat_models=EXCLUDED.chat_models, updated_at=EXCLUDED.updated_at RETURNING updated_at`, config.BaseURL, config.APIKey, config.Model, config.ChatModel, encodeModelList(config.ImageModels), encodeModelList(config.ChatModels), config.UpdatedAt).Scan(&config.UpdatedAt)
	return config, err
}

func encodeModelList(models []string) []byte {
	if models == nil {
		models = []string{}
	}
	data, _ := json.Marshal(models)
	return data
}

func decodeModelList(raw, fallback string) []string {
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		models = nil
	}
	if len(models) == 0 && fallback != "" {
		return []string{fallback}
	}
	return models
}

func (s *PostgresStore) ListCanvases(ctx context.Context, userID string) ([]api.CanvasSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, title, created_at, updated_at, revision FROM artisan_canvases WHERE user_id=$1 ORDER BY updated_at DESC, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	canvases := []api.CanvasSummary{}
	for rows.Next() {
		var canvas api.CanvasSummary
		if err := rows.Scan(&canvas.ID, &canvas.Title, &canvas.CreatedAt, &canvas.UpdatedAt, &canvas.Revision); err != nil {
			return nil, err
		}
		canvases = append(canvases, canvas)
	}
	return canvases, rows.Err()
}

func (s *PostgresStore) CreateCanvas(ctx context.Context, userID string, title string, state json.RawMessage) (api.CanvasResponse, error) {
	if title == "" {
		title = "默认画布"
	}
	if len(state) == 0 {
		state = json.RawMessage(`{}`)
	}
	canvas := api.CanvasResponse{ID: uuid.NewString(), Title: title, State: state}
	err := s.pool.QueryRow(ctx, `INSERT INTO artisan_canvases (id, user_id, title, state_json) VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at, revision`, canvas.ID, userID, title, state).Scan(&canvas.CreatedAt, &canvas.UpdatedAt, &canvas.Revision)
	return canvas, err
}

func (s *PostgresStore) GetCanvas(ctx context.Context, userID string, canvasID string) (api.CanvasResponse, error) {
	var canvas api.CanvasResponse
	err := s.pool.QueryRow(ctx, `SELECT id::text, title, state_json, created_at, updated_at, revision FROM artisan_canvases WHERE id=$1 AND user_id=$2`, canvasID, userID).Scan(&canvas.ID, &canvas.Title, &canvas.State, &canvas.CreatedAt, &canvas.UpdatedAt, &canvas.Revision)
	return canvas, err
}

func (s *PostgresStore) GetCurrentCanvas(ctx context.Context, userID string) (api.CanvasResponse, error) {
	var canvas api.CanvasResponse
	err := s.pool.QueryRow(ctx, `SELECT id::text, title, state_json, created_at, updated_at, revision FROM artisan_canvases WHERE user_id=$1 ORDER BY updated_at DESC, created_at DESC LIMIT 1`, userID).Scan(&canvas.ID, &canvas.Title, &canvas.State, &canvas.CreatedAt, &canvas.UpdatedAt, &canvas.Revision)
	return canvas, err
}

func (s *PostgresStore) SaveCanvas(ctx context.Context, userID string, canvasID string, request api.SaveCanvasRequest) (api.CanvasResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return api.CanvasResponse{}, err
	}
	defer tx.Rollback(ctx)
	var canvas api.CanvasResponse
	err = tx.QueryRow(ctx, `SELECT id::text, title, state_json, created_at, updated_at, revision FROM artisan_canvases WHERE id=$1 AND user_id=$2 FOR UPDATE`, canvasID, userID).Scan(&canvas.ID, &canvas.Title, &canvas.State, &canvas.CreatedAt, &canvas.UpdatedAt, &canvas.Revision)
	if err != nil {
		return api.CanvasResponse{}, err
	}
	if request.ClientRevision != nil && *request.ClientRevision < canvas.Revision {
		return canvas, tx.Commit(ctx)
	}
	if request.Title == "" {
		request.Title = canvas.Title
	}
	nextRevision := canvas.Revision + 1
	err = tx.QueryRow(ctx, `UPDATE artisan_canvases SET title=$1, state_json=$2, revision=$3, updated_at=NOW() WHERE id=$4 AND user_id=$5 RETURNING title, state_json, updated_at, revision`, request.Title, request.State, nextRevision, canvasID, userID).Scan(&canvas.Title, &canvas.State, &canvas.UpdatedAt, &canvas.Revision)
	if err != nil {
		return api.CanvasResponse{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO artisan_canvas_snapshots (id, canvas_id, state_json, revision) VALUES ($1, $2, $3, $4)`, uuid.NewString(), canvasID, request.State, canvas.Revision)
	if err != nil {
		return api.CanvasResponse{}, err
	}
	return canvas, tx.Commit(ctx)
}

func (s *PostgresStore) CreateAsset(ctx context.Context, asset api.AssetResponse) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO artisan_assets (id, canvas_id, user_id, object_key, url_path, original_filename, mime_type, size_bytes, width, height, created_at) VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, asset.ID, asset.CanvasID, asset.UserID, asset.ObjectKey, asset.URLPath, asset.OriginalFilename, asset.MimeType, asset.SizeBytes, asset.Width, asset.Height, asset.CreatedAt)
	return err
}

func (s *PostgresStore) GetAsset(ctx context.Context, assetID string) (api.AssetResponse, error) {
	var asset api.AssetResponse
	err := s.pool.QueryRow(ctx, `SELECT id::text, COALESCE(canvas_id::text, ''), COALESCE(user_id::text, ''), object_key, url_path, original_filename, mime_type, size_bytes, width, height, created_at FROM artisan_assets WHERE id=$1`, assetID).Scan(&asset.ID, &asset.CanvasID, &asset.UserID, &asset.ObjectKey, &asset.URLPath, &asset.OriginalFilename, &asset.MimeType, &asset.SizeBytes, &asset.Width, &asset.Height, &asset.CreatedAt)
	asset.PreviewURLPath = "/api/assets/" + asset.ID + "/preview"
	asset.ThumbnailURLPath = "/api/assets/" + asset.ID + "/thumbnail"
	return asset, err
}

func (s *PostgresStore) SaveExport(ctx context.Context, export api.ExportFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exports[export.ID] = export
	return nil
}

func (s *PostgresStore) GetExport(ctx context.Context, userID string, exportID string) (api.ExportFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	export, ok := s.exports[exportID]
	if !ok || export.UserID != userID {
		return api.ExportFile{}, fmt.Errorf("export not found")
	}
	return export, nil
}

func (s *PostgresStore) AdminSummary(ctx context.Context) (api.AdminSummary, error) {
	var summary api.AdminSummary
	err := s.pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM artisan_users),
  (SELECT COUNT(*) FROM artisan_users WHERE is_admin = TRUE),
  (SELECT COUNT(*) FROM artisan_canvases),
  (SELECT COUNT(*) FROM artisan_assets)
`).Scan(&summary.UserCount, &summary.AdminCount, &summary.CanvasCount, &summary.AssetCount)
	return summary, err
}

func (s *PostgresStore) AdminListUsers(ctx context.Context) ([]api.AuthUser, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, username, display_name, is_admin, created_at FROM artisan_users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []api.AuthUser{}
	for rows.Next() {
		var user api.AuthUser
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.IsAdmin, &user.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *PostgresStore) AdminListCanvases(ctx context.Context) ([]api.AdminCanvas, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id::text, c.title, COALESCE(c.user_id::text, ''), COALESCE(u.username, ''), c.created_at, c.updated_at, c.revision FROM artisan_canvases c LEFT JOIN artisan_users u ON u.id = c.user_id ORDER BY c.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	canvases := []api.AdminCanvas{}
	for rows.Next() {
		var canvas api.AdminCanvas
		if err := rows.Scan(&canvas.ID, &canvas.Title, &canvas.UserID, &canvas.Username, &canvas.CreatedAt, &canvas.UpdatedAt, &canvas.Revision); err != nil {
			return nil, err
		}
		canvases = append(canvases, canvas)
	}
	return canvases, rows.Err()
}

func (s *PostgresStore) AdminListAssets(ctx context.Context) ([]api.AssetResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, COALESCE(canvas_id::text, ''), COALESCE(user_id::text, ''), object_key, url_path, original_filename, mime_type, size_bytes, width, height, created_at FROM artisan_assets ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := []api.AssetResponse{}
	for rows.Next() {
		var asset api.AssetResponse
		if err := rows.Scan(&asset.ID, &asset.CanvasID, &asset.UserID, &asset.ObjectKey, &asset.URLPath, &asset.OriginalFilename, &asset.MimeType, &asset.SizeBytes, &asset.Width, &asset.Height, &asset.CreatedAt); err != nil {
			return nil, err
		}
		asset.PreviewURLPath = "/api/assets/" + asset.ID + "/preview"
		asset.ThumbnailURLPath = "/api/assets/" + asset.ID + "/thumbnail"
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func joinURL(baseURL, path string) string {
	if baseURL == "" {
		return ""
	}
	return baseURL + "/" + path
}
