package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type TestServer struct {
	*Server
}

func NewTestServer(t *testing.T) *TestServer {
	t.Helper()
	server := NewServer(Config{JWTSecret: "test-secret-at-least-32-bytes", ExportTTL: time.Hour}, newMemoryStore(), newMemoryObjectStore())
	return &TestServer{Server: server}
}

type memoryStore struct {
	mu              sync.RWMutex
	usersByID       map[string]UserRecord
	usersByName     map[string]string
	configs         map[string]APIConfigResponse
	systemConfig    APIConfigResponse
	hasSystemConfig bool
	canvases        map[string]CanvasResponse
	canvasUsers     map[string]string
	assets          map[string]AssetResponse
	exports         map[string]ExportFile
}

func newMemoryStore() *memoryStore {
	return &memoryStore{usersByID: map[string]UserRecord{}, usersByName: map[string]string{}, configs: map[string]APIConfigResponse{}, canvases: map[string]CanvasResponse{}, canvasUsers: map[string]string{}, assets: map[string]AssetResponse{}, exports: map[string]ExportFile{}}
}

func (m *memoryStore) CountUsers(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.usersByID), nil
}

func (m *memoryStore) CreateUser(ctx context.Context, user UserRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.usersByName[user.Username]; exists {
		return fmt.Errorf("username exists")
	}
	m.usersByID[user.ID] = user
	m.usersByName[user.Username] = user.ID
	return nil
}

func (m *memoryStore) FindUserByUsername(ctx context.Context, username string) (UserRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.usersByName[username]
	if !ok {
		return UserRecord{}, errNotFound
	}
	return m.usersByID[id], nil
}

func (m *memoryStore) FindUserByID(ctx context.Context, id string) (UserRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.usersByID[id]
	if !ok {
		return UserRecord{}, errNotFound
	}
	return user, nil
}

func (m *memoryStore) GetAPIConfig(ctx context.Context, userID string) (APIConfigResponse, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	config, ok := m.configs[userID]
	return config, ok, nil
}

func (m *memoryStore) SaveAPIConfig(ctx context.Context, userID string, config APIConfigResponse) (APIConfigResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	config.UpdatedAt = time.Now().UTC()
	m.configs[userID] = config
	return config, nil
}

func (m *memoryStore) GetSystemAPIConfig(ctx context.Context) (APIConfigResponse, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.systemConfig, m.hasSystemConfig, nil
}

func (m *memoryStore) SaveSystemAPIConfig(ctx context.Context, config APIConfigResponse) (APIConfigResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	config.UpdatedAt = time.Now().UTC()
	config.ServerDefault = true
	m.systemConfig = config
	m.hasSystemConfig = true
	return config, nil
}

func (m *memoryStore) ListCanvases(ctx context.Context, userID string) ([]CanvasSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	canvases := []CanvasSummary{}
	for id, canvas := range m.canvases {
		if m.canvasUsers[id] == userID {
			canvases = append(canvases, CanvasSummary{ID: canvas.ID, Title: canvas.Title, CreatedAt: canvas.CreatedAt, UpdatedAt: canvas.UpdatedAt, Revision: canvas.Revision})
		}
	}
	sortCanvases(canvases)
	return canvases, nil
}

func (m *memoryStore) CreateCanvas(ctx context.Context, userID string, title string, state json.RawMessage) (CanvasResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if title == "" {
		title = "默认画布"
	}
	if len(state) == 0 {
		state = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	canvas := CanvasResponse{ID: uuid.NewString(), Title: title, State: state, CreatedAt: now, UpdatedAt: now, Revision: 0, Queued: false}
	m.canvases[canvas.ID] = canvas
	m.canvasUsers[canvas.ID] = userID
	return canvas, nil
}

func (m *memoryStore) GetCanvas(ctx context.Context, userID string, canvasID string) (CanvasResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	canvas, ok := m.canvases[canvasID]
	if !ok || m.canvasUsers[canvasID] != userID {
		return CanvasResponse{}, errNotFound
	}
	return canvas, nil
}

func (m *memoryStore) GetCurrentCanvas(ctx context.Context, userID string) (CanvasResponse, error) {
	canvases, _ := m.ListCanvases(ctx, userID)
	if len(canvases) == 0 {
		return CanvasResponse{}, errNotFound
	}
	return m.GetCanvas(ctx, userID, canvases[0].ID)
}

func (m *memoryStore) SaveCanvas(ctx context.Context, userID string, canvasID string, request SaveCanvasRequest) (CanvasResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	canvas, ok := m.canvases[canvasID]
	if !ok || m.canvasUsers[canvasID] != userID {
		return CanvasResponse{}, errNotFound
	}
	if request.ClientRevision != nil && *request.ClientRevision < canvas.Revision {
		return canvas, nil
	}
	if request.Title != "" {
		canvas.Title = request.Title
	}
	canvas.State = request.State
	canvas.Revision++
	canvas.UpdatedAt = time.Now().UTC()
	m.canvases[canvasID] = canvas
	return canvas, nil
}

func (m *memoryStore) DeleteCanvas(ctx context.Context, userID string, canvasID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.canvases[canvasID]; !ok || m.canvasUsers[canvasID] != userID {
		return errNotFound
	}
	delete(m.canvases, canvasID)
	delete(m.canvasUsers, canvasID)
	return nil
}

func (m *memoryStore) CreateAsset(ctx context.Context, asset AssetResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assets[asset.ID] = asset
	return nil
}

func (m *memoryStore) GetAsset(ctx context.Context, assetID string) (AssetResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	asset, ok := m.assets[assetID]
	if !ok {
		return AssetResponse{}, errNotFound
	}
	return asset, nil
}

func (m *memoryStore) SaveExport(ctx context.Context, export ExportFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exports[export.ID] = export
	return nil
}

func (m *memoryStore) GetExport(ctx context.Context, userID string, exportID string) (ExportFile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	export, ok := m.exports[exportID]
	if !ok || export.UserID != userID {
		return ExportFile{}, errNotFound
	}
	return export, nil
}

func (m *memoryStore) AdminSummary(ctx context.Context) (AdminSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	adminCount := 0
	for _, user := range m.usersByID {
		if user.IsAdmin {
			adminCount++
		}
	}
	return AdminSummary{UserCount: len(m.usersByID), AdminCount: adminCount, CanvasCount: len(m.canvases), AssetCount: len(m.assets)}, nil
}

func (m *memoryStore) AdminListUsers(ctx context.Context) ([]AuthUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users := make([]AuthUser, 0, len(m.usersByID))
	for _, user := range m.usersByID {
		users = append(users, user.AuthUser)
	}
	return users, nil
}

func (m *memoryStore) AdminListCanvases(ctx context.Context) ([]AdminCanvas, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	canvases := make([]AdminCanvas, 0, len(m.canvases))
	for id, canvas := range m.canvases {
		userID := m.canvasUsers[id]
		user := m.usersByID[userID]
		canvases = append(canvases, AdminCanvas{ID: canvas.ID, Title: canvas.Title, UserID: userID, Username: user.Username, CreatedAt: canvas.CreatedAt, UpdatedAt: canvas.UpdatedAt, Revision: canvas.Revision})
	}
	return canvases, nil
}

func (m *memoryStore) AdminListAssets(ctx context.Context) ([]AssetResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	assets := make([]AssetResponse, 0, len(m.assets))
	for _, asset := range m.assets {
		assets = append(assets, asset)
	}
	return assets, nil
}

type memoryObjectStore struct {
	mu          sync.RWMutex
	objects     map[string][]byte
	contentType map[string]string
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{objects: map[string][]byte{}, contentType: map[string]string{}}
}

func (m *memoryObjectStore) Put(ctx context.Context, key string, contentType string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = append([]byte(nil), body...)
	m.contentType[key] = contentType
	return nil
}

func (m *memoryObjectStore) Get(ctx context.Context, key string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	body, ok := m.objects[key]
	if !ok {
		return nil, "", errNotFound
	}
	return append([]byte(nil), body...), m.contentType[key], nil
}
