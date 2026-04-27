package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userContextKey contextKey = "livart-user"

type Store interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, user UserRecord) error
	FindUserByUsername(ctx context.Context, username string) (UserRecord, error)
	FindUserByID(ctx context.Context, id string) (UserRecord, error)
	GetAPIConfig(ctx context.Context, userID string) (APIConfigResponse, bool, error)
	SaveAPIConfig(ctx context.Context, userID string, config APIConfigResponse) (APIConfigResponse, error)
	GetSystemAPIConfig(ctx context.Context) (APIConfigResponse, bool, error)
	SaveSystemAPIConfig(ctx context.Context, config APIConfigResponse) (APIConfigResponse, error)
	ListCanvases(ctx context.Context, userID string) ([]CanvasSummary, error)
	CreateCanvas(ctx context.Context, userID string, title string, state json.RawMessage) (CanvasResponse, error)
	GetCanvas(ctx context.Context, userID string, canvasID string) (CanvasResponse, error)
	GetCurrentCanvas(ctx context.Context, userID string) (CanvasResponse, error)
	SaveCanvas(ctx context.Context, userID string, canvasID string, request SaveCanvasRequest) (CanvasResponse, error)
	DeleteCanvas(ctx context.Context, userID string, canvasID string) error
	CreateAsset(ctx context.Context, asset AssetResponse) error
	GetAsset(ctx context.Context, assetID string) (AssetResponse, error)
	SaveExport(ctx context.Context, export ExportFile) error
	GetExport(ctx context.Context, userID string, exportID string) (ExportFile, error)
	AdminSummary(ctx context.Context) (AdminSummary, error)
	AdminListUsers(ctx context.Context) ([]AuthUser, error)
	AdminListCanvases(ctx context.Context) ([]AdminCanvas, error)
	AdminListAssets(ctx context.Context) ([]AssetResponse, error)
}

type ObjectStore interface {
	Put(ctx context.Context, key string, contentType string, body []byte) error
	Get(ctx context.Context, key string) ([]byte, string, error)
}

type Server struct {
	Handler http.Handler
	Config  *Config
	store   Store
	objects ObjectStore
	jobs    map[string]ImageJob
	jobsMu  sync.RWMutex
	ws      *websocket.Upgrader
	clients map[string]map[*websocket.Conn]bool
	wsMu    sync.Mutex
}

func NewServer(config Config, store Store, objects ObjectStore) *Server {
	config.normalize()
	server := &Server{
		Config:  &config,
		store:   store,
		objects: objects,
		jobs:    map[string]ImageJob{},
		ws: &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
			return true
		}},
		clients: map[string]map[*websocket.Conn]bool{},
	}
	server.Handler = server.routes()
	return server
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.cors)
	r.Use(s.requestID)

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, map[string]string{"status": "ok"})
	})
	r.Post("/api/auth/register", s.register)
	r.Post("/api/auth/login", s.login)
	r.Get("/api/assets/{id}/content", s.assetContent)
	r.Get("/api/assets/{id}/preview", s.assetContent)
	r.Get("/api/assets/{id}/thumbnail", s.assetContent)
	r.Get("/api/assets/{id}/view/{width}", s.assetContent)
	r.Get("/ws/image-jobs", s.imageJobWebSocket)

	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.Get("/api/auth/me", s.me)
		r.Post("/api/auth/logout", s.logout)
		r.Get("/api/user/config", s.getAPIConfig)
		r.Put("/api/user/config", s.saveAPIConfig)
		r.Get("/api/canvases", s.listCanvases)
		r.Post("/api/canvases", s.createCanvas)
		r.Get("/api/canvases/{id}", s.getCanvas)
		r.Put("/api/canvases/{id}", s.saveCanvas)
		r.Delete("/api/canvases/{id}", s.deleteCanvas)
		r.Get("/api/canvas/current", s.getCurrentCanvas)
		r.Put("/api/canvas/current", s.saveCurrentCanvas)
		r.Post("/api/assets", s.uploadAsset)
		r.Post("/api/images/generations", s.proxyImageGeneration)
		r.Post("/api/images/edits", s.proxyImageEdit)
		r.Post("/api/image-jobs/generations", s.createImageGenerationJob)
		r.Post("/api/image-jobs/edits", s.createImageEditJob)
		r.Get("/api/image-jobs/{jobId}", s.getImageJob)
		r.Post("/api/image-references/analyze", s.analyzeImageReferences)
		r.Post("/api/exports/images", s.createImageExport)
		r.Get("/api/exports/{exportId}/download", s.downloadExport)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.Use(s.adminOnly)
		r.Get("/api/admin/config", s.adminGetConfig)
		r.Put("/api/admin/config", s.adminSaveConfig)
		r.Get("/api/admin/summary", s.adminSummary)
		r.Get("/api/admin/users", s.adminUsers)
		r.Get("/api/admin/canvases", s.adminCanvases)
		r.Get("/api/admin/assets", s.adminAssets)
	})
	r.Get("/*", s.staticFrontend)

	return r
}

func (s *Server) staticFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		writeFail(w, http.StatusNotFound, "接口不存在", "NOT_FOUND")
		return
	}
	if s.Config.StaticDir == "" {
		http.NotFound(w, r)
		return
	}

	requestedPath := filepath.Clean("/" + r.URL.Path)
	filePath := filepath.Join(s.Config.StaticDir, strings.TrimPrefix(requestedPath, "/"))
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, filePath)
		return
	}

	indexPath := filepath.Join(s.Config.StaticDir, "index.html")
	if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, indexPath)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Livart-Api-Key, X-Livart-Upstream-Url")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	if len(s.Config.AllowedCORSOrigins) == 0 {
		return true
	}
	for _, allowed := range s.Config.AllowedCORSOrigins {
		if allowed == "*" || subtle.ConstantTimeCompare([]byte(allowed), []byte(origin)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", requestID)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			writeFail(w, http.StatusUnauthorized, "请先登录", "AUTH_REQUIRED")
			return
		}
		userID, err := s.verifyToken(token)
		if err != nil {
			writeFail(w, http.StatusUnauthorized, "登录状态已失效，请重新登录", "INVALID_TOKEN")
			return
		}
		user, err := s.store.FindUserByID(r.Context(), userID)
		if err != nil {
			writeFail(w, http.StatusUnauthorized, "登录状态已失效，请重新登录", "INVALID_TOKEN")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user.AuthUser)))
	})
}

func (s *Server) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			writeFail(w, http.StatusForbidden, "需要管理员权限", "ADMIN_REQUIRED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) AuthUser {
	user, _ := r.Context().Value(userContextKey).(AuthUser)
	return user
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var request RegisterRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if len(request.Username) < 3 || len(request.Password) < 6 {
		writeFail(w, http.StatusBadRequest, "用户名至少 3 位，密码至少 6 位", "VALIDATION_ERROR")
		return
	}
	if request.DisplayName == "" {
		request.DisplayName = request.Username
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "密码加密失败", "PASSWORD_HASH_FAILED")
		return
	}
	now := time.Now().UTC()
	userCount, err := s.store.CountUsers(r.Context())
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取用户数量失败", "USER_COUNT_FAILED")
		return
	}
	user := UserRecord{AuthUser: AuthUser{ID: uuid.NewString(), Username: request.Username, DisplayName: request.DisplayName, IsAdmin: userCount == 0, CreatedAt: now}, PasswordHash: string(hash)}
	if err := s.store.CreateUser(r.Context(), user); err != nil {
		writeFail(w, http.StatusConflict, "用户名已存在", "USERNAME_EXISTS")
		return
	}
	s.writeAuthResponse(w, user.AuthUser)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	user, err := s.store.FindUserByUsername(r.Context(), strings.TrimSpace(request.Username))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)) != nil {
		writeFail(w, http.StatusUnauthorized, "用户名或密码错误", "INVALID_CREDENTIALS")
		return
	}
	s.writeAuthResponse(w, user.AuthUser)
}

func (s *Server) writeAuthResponse(w http.ResponseWriter, user AuthUser) {
	expiresAt := time.Now().UTC().Add(time.Duration(s.Config.JWTTTLDays) * 24 * time.Hour)
	token, err := s.signToken(user.ID, expiresAt)
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "创建登录令牌失败", "TOKEN_CREATE_FAILED")
		return
	}
	writeOK(w, AuthResponse{User: user, Token: token, ExpiresAt: expiresAt})
}

func (s *Server) signToken(userID string, expiresAt time.Time) (string, error) {
	claims := jwt.MapClaims{"sub": userID, "exp": expiresAt.Unix(), "iat": time.Now().UTC().Unix()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.Config.JWTSecret))
}

func (s *Server) verifyToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.Config.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims")
	}
	userID, _ := claims["sub"].(string)
	if userID == "" {
		return "", fmt.Errorf("missing subject")
	}
	return userID, nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) { writeOK(w, currentUser(r)) }

func (s *Server) logout(w http.ResponseWriter, r *http.Request) { writeOK[any](w, nil) }

func (s *Server) getAPIConfig(w http.ResponseWriter, r *http.Request) {
	config, ok, err := s.publicSiteAPIConfig(r.Context())
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取用户配置失败", "CONFIG_READ_FAILED")
		return
	}
	if !ok {
		writeOK[*APIConfigResponse](w, nil)
		return
	}
	config.APIKey = ""
	config.ServerDefault = true
	writeOK(w, config)
}

func (s *Server) saveAPIConfig(w http.ResponseWriter, r *http.Request) {
	writeFail(w, http.StatusForbidden, "公益站点由管理员统一配置中转站", "USER_CONFIG_DISABLED")
}

func (s *Server) buildAPIConfig(baseURL, apiKey, model, chatModel string, imageModels, chatModels []string, serverDefault bool) APIConfigResponse {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if model == "" {
		model = s.Config.DefaultImageModel
	}
	if chatModel == "" {
		chatModel = s.Config.DefaultChatModel
	}
	return APIConfigResponse{BaseURL: baseURL, APIKey: strings.TrimSpace(apiKey), Model: model, ChatModel: chatModel, ImageModels: normalizeModelList(imageModels, model), ChatModels: normalizeModelList(chatModels, chatModel), TextToImageURL: joinURL(baseURL, "images/generations"), ImageToImageURL: joinURL(baseURL, "images/edits"), UpdatedAt: time.Now().UTC(), ServerDefault: serverDefault}
}

func normalizeModelList(models []string, selected string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(models)+1)
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		normalized = append(normalized, model)
	}
	selected = strings.TrimSpace(selected)
	if len(normalized) == 0 && selected != "" {
		normalized = append(normalized, selected)
	}
	return normalized
}

func modelInList(model string, models []string) bool {
	model = strings.TrimSpace(model)
	for _, candidate := range models {
		if strings.TrimSpace(candidate) == model {
			return true
		}
	}
	return false
}

func (s *Server) publicSiteAPIConfig(ctx context.Context) (APIConfigResponse, bool, error) {
	config, ok, err := s.store.GetSystemAPIConfig(ctx)
	if err != nil {
		return APIConfigResponse{}, false, err
	}
	if ok {
		config.ServerDefault = true
		return config, true, nil
	}
	if s.Config.DefaultAPIBaseURL == "" || s.Config.DefaultAPIKey == "" {
		return APIConfigResponse{}, false, nil
	}
	return s.buildAPIConfig(s.Config.DefaultAPIBaseURL, s.Config.DefaultAPIKey, s.Config.DefaultImageModel, s.Config.DefaultChatModel, nil, nil, true), true, nil
}

func (s *Server) upstreamAPIConfig(ctx context.Context) (APIConfigResponse, bool) {
	config, ok, err := s.publicSiteAPIConfig(ctx)
	if err != nil || !ok || config.BaseURL == "" || config.APIKey == "" {
		return APIConfigResponse{}, false
	}
	return config, true
}

func joinURL(baseURL, path string) string {
	if baseURL == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (s *Server) listCanvases(w http.ResponseWriter, r *http.Request) {
	canvases, err := s.store.ListCanvases(r.Context(), currentUser(r).ID)
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取项目列表失败", "CANVAS_LIST_FAILED")
		return
	}
	writeOK(w, canvases)
}

func (s *Server) createCanvas(w http.ResponseWriter, r *http.Request) {
	var request CreateCanvasRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	canvas, err := s.store.CreateCanvas(r.Context(), currentUser(r).ID, request.Title, json.RawMessage(`{}`))
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "创建画布失败", "CANVAS_CREATE_FAILED")
		return
	}
	writeOKStatus(w, http.StatusCreated, canvas)
}

func (s *Server) getCanvas(w http.ResponseWriter, r *http.Request) {
	canvas, err := s.store.GetCanvas(r.Context(), currentUser(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		writeFail(w, http.StatusNotFound, "画布不存在", "CANVAS_NOT_FOUND")
		return
	}
	writeOK(w, canvas)
}

func (s *Server) getCurrentCanvas(w http.ResponseWriter, r *http.Request) {
	canvas, err := s.store.GetCurrentCanvas(r.Context(), currentUser(r).ID)
	if err != nil {
		canvas, err = s.store.CreateCanvas(r.Context(), currentUser(r).ID, "默认画布", json.RawMessage(`{}`))
	}
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取默认画布失败", "CANVAS_READ_FAILED")
		return
	}
	writeOK(w, canvas)
}

func (s *Server) saveCanvas(w http.ResponseWriter, r *http.Request) {
	s.saveCanvasByID(w, r, chi.URLParam(r, "id"))
}

func (s *Server) deleteCanvas(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCanvas(r.Context(), currentUser(r).ID, chi.URLParam(r, "id")); err != nil {
		writeFail(w, http.StatusNotFound, "画布不存在", "CANVAS_NOT_FOUND")
		return
	}
	writeOK(w, map[string]bool{"deleted": true})
}

func (s *Server) saveCurrentCanvas(w http.ResponseWriter, r *http.Request) {
	canvas, err := s.store.GetCurrentCanvas(r.Context(), currentUser(r).ID)
	if err != nil {
		writeFail(w, http.StatusNotFound, "当前项目未加载，不能保存画布", "CANVAS_NOT_FOUND")
		return
	}
	s.saveCanvasByID(w, r, canvas.ID)
}

func (s *Server) saveCanvasByID(w http.ResponseWriter, r *http.Request, canvasID string) {
	var request SaveCanvasRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.State) == 0 || !json.Valid(request.State) {
		writeFail(w, http.StatusBadRequest, "画布状态不能为空", "VALIDATION_ERROR")
		return
	}
	canvas, err := s.store.SaveCanvas(r.Context(), currentUser(r).ID, canvasID, request)
	if err != nil {
		writeFail(w, http.StatusNotFound, "画布不存在", "CANVAS_NOT_FOUND")
		return
	}
	canvas.Queued = true
	writeOKStatus(w, http.StatusAccepted, canvas)
}

func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.Config.MaxUploadBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeFail(w, http.StatusBadRequest, "请选择要上传的图片", "ASSET_FILE_REQUIRED")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeFail(w, http.StatusBadRequest, "读取上传文件失败", "ASSET_READ_FAILED")
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	assetID := uuid.NewString()
	key := fmt.Sprintf("assets/%s/%s/%s", currentUser(r).ID, assetID, safeFilename(header.Filename))
	if err := s.objects.Put(r.Context(), key, contentType, data); err != nil {
		writeFail(w, http.StatusInternalServerError, "保存图片资源失败", "ASSET_STORE_FAILED")
		return
	}
	asset := AssetResponse{ID: assetID, CanvasID: r.FormValue("canvasId"), UserID: currentUser(r).ID, URLPath: "/api/assets/" + assetID + "/content", PreviewURLPath: "/api/assets/" + assetID + "/preview", ThumbnailURLPath: "/api/assets/" + assetID + "/thumbnail", OriginalFilename: header.Filename, MimeType: contentType, SizeBytes: int64(len(data)), CreatedAt: time.Now().UTC(), ObjectKey: key}
	if err := s.store.CreateAsset(r.Context(), asset); err != nil {
		writeFail(w, http.StatusInternalServerError, "保存图片元数据失败", "ASSET_METADATA_FAILED")
		return
	}
	writeOK(w, asset)
}

func (s *Server) assetContent(w http.ResponseWriter, r *http.Request) {
	asset, err := s.store.GetAsset(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, contentType, err := s.objects.Get(r.Context(), asset.ObjectKey)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType == "" {
		contentType = asset.MimeType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) proxyImageGeneration(w http.ResponseWriter, r *http.Request) {
	s.proxyImageRequest(w, r, "images/generations")
}

func (s *Server) proxyImageEdit(w http.ResponseWriter, r *http.Request) {
	s.proxyImageRequest(w, r, "images/edits")
}

func (s *Server) proxyImageRequest(w http.ResponseWriter, r *http.Request, fallbackPath string) {
	body, contentType, originalPrompt, err := s.readImageRequestBody(r)
	if err != nil {
		writeFail(w, http.StatusBadRequest, err.Error(), "IMAGE_REQUEST_INVALID")
		return
	}
	upstreamURL := r.Header.Get("X-Livart-Upstream-Url")
	apiKey := r.Header.Get("X-Livart-Api-Key")
	if upstreamURL == "" || apiKey == "" {
		config, ok := s.upstreamAPIConfig(r.Context())
		if ok {
			upstreamURL = map[bool]string{true: config.TextToImageURL, false: config.ImageToImageURL}[fallbackPath == "images/generations"]
			apiKey = config.APIKey
		}
	}
	if upstreamURL == "" || apiKey == "" {
		writeFail(w, http.StatusBadRequest, "请先配置 API 地址和密钥", "API_CONFIG_REQUIRED")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.Config.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeFail(w, http.StatusBadRequest, "上游地址无效", "UPSTREAM_URL_INVALID")
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeFail(w, http.StatusBadGateway, "上游 AI 接口请求失败："+err.Error(), "UPSTREAM_REQUEST_FAILED")
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", firstNonEmpty(resp.Header.Get("Content-Type"), "application/json"))
	writePromptHeaders(w, originalPrompt, originalPrompt)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (s *Server) readImageRequestBody(r *http.Request) ([]byte, string, string, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return s.rewriteMultipartImageRequest(r)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, "", "", err
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	originalPrompt, _ := payload["prompt"].(string)
	return body, firstNonEmpty(contentType, "application/json"), originalPrompt, nil
}

func (s *Server) rewriteMultipartImageRequest(r *http.Request) ([]byte, string, string, error) {
	if err := r.ParseMultipartForm(s.Config.MaxUploadBytes); err != nil {
		return nil, "", "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	originalPrompt := r.FormValue("prompt")
	for key, values := range r.MultipartForm.Value {
		if key == "imageAssetId" || key == "referenceAssetId" {
			continue
		}
		for _, value := range values {
			_ = writer.WriteField(key, value)
		}
	}
	copyFiles := func(field string, files []*multipart.FileHeader) error {
		for _, header := range files {
			file, err := header.Open()
			if err != nil {
				return err
			}
			part, err := writer.CreateFormFile(field, header.Filename)
			if err != nil {
				file.Close()
				return err
			}
			_, err = io.Copy(part, file)
			file.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	for field, files := range r.MultipartForm.File {
		if err := copyFiles(field, files); err != nil {
			return nil, "", "", err
		}
	}
	assetIDs := append([]string{}, r.MultipartForm.Value["imageAssetId"]...)
	assetIDs = append(assetIDs, r.MultipartForm.Value["referenceAssetId"]...)
	for index, assetID := range assetIDs {
		asset, err := s.store.GetAsset(r.Context(), assetID)
		if err != nil {
			return nil, "", "", fmt.Errorf("图片资源不存在：%s", assetID)
		}
		data, contentType, err := s.objects.Get(r.Context(), asset.ObjectKey)
		if err != nil {
			return nil, "", "", err
		}
		field := "reference_image"
		if index == 0 {
			field = "image"
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, safeFilename(asset.OriginalFilename)))
		header.Set("Content-Type", firstNonEmpty(contentType, asset.MimeType))
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", "", err
		}
		_, _ = part.Write(data)
	}
	if err := writer.Close(); err != nil {
		return nil, "", "", err
	}
	return body.Bytes(), writer.FormDataContentType(), originalPrompt, nil
}

func (s *Server) createImageGenerationJob(w http.ResponseWriter, r *http.Request) {
	s.createImageJob(w, r, "images/generations")
}

func (s *Server) createImageEditJob(w http.ResponseWriter, r *http.Request) {
	s.createImageJob(w, r, "images/edits")
}

func (s *Server) createImageJob(w http.ResponseWriter, r *http.Request, fallbackPath string) {
	body, contentType, prompt, err := s.readImageRequestBody(r)
	if err != nil {
		writeFail(w, http.StatusBadRequest, err.Error(), "IMAGE_REQUEST_INVALID")
		return
	}
	config, ok := s.upstreamAPIConfig(r.Context())
	if !ok {
		writeFail(w, http.StatusBadRequest, "请先配置 API 地址和密钥", "API_CONFIG_REQUIRED")
		return
	}
	upstreamURL := config.ImageToImageURL
	if fallbackPath == "images/generations" {
		upstreamURL = config.TextToImageURL
	}
	if upstreamURL == "" || config.APIKey == "" {
		writeFail(w, http.StatusBadRequest, "请先配置 API 地址和密钥", "API_CONFIG_REQUIRED")
		return
	}

	job := ImageJob{JobID: uuid.NewString(), Status: "pending", OriginalPrompt: prompt, OptimizedPrompt: prompt, Attempts: 1, UserID: currentUser(r).ID}
	s.jobsMu.Lock()
	s.jobs[job.JobID] = job
	s.jobsMu.Unlock()
	s.publishJob(job)
	go s.runImageJob(job.JobID, upstreamURL, config.APIKey, contentType, body)
	writeRawJSON(w, http.StatusAccepted, job)
}

func (s *Server) runImageJob(jobID, upstreamURL, apiKey, contentType string, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), s.Config.RequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		s.completeImageJob(jobID, ImageJob{Status: "error", Error: map[string]any{"message": "上游地址无效", "code": "UPSTREAM_URL_INVALID"}})
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.completeImageJob(jobID, ImageJob{Status: "error", Error: map[string]any{"message": "上游 AI 接口请求失败：" + err.Error(), "code": "UPSTREAM_REQUEST_FAILED"}})
		return
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	requestID := firstNonEmpty(resp.Header.Get("X-Request-Id"), resp.Header.Get("Openai-Request-Id"), resp.Header.Get("Request-Id"))
	if readErr != nil {
		s.completeImageJob(jobID, ImageJob{Status: "error", Error: map[string]any{"message": "读取上游响应失败：" + readErr.Error(), "code": "UPSTREAM_RESPONSE_READ_FAILED"}, UpstreamStatus: resp.StatusCode, RequestID: requestID})
		return
	}

	status := "completed"
	var jobError any
	if resp.StatusCode >= 400 {
		status = "error"
		jobError = json.RawMessage(respBody)
	}
	s.completeImageJob(jobID, ImageJob{Status: status, Response: json.RawMessage(respBody), Error: jobError, UpstreamStatus: resp.StatusCode, RequestID: requestID})
}

func (s *Server) completeImageJob(jobID string, patch ImageJob) {
	s.jobsMu.Lock()
	job, ok := s.jobs[jobID]
	if !ok {
		s.jobsMu.Unlock()
		return
	}
	job.Status = patch.Status
	job.Response = patch.Response
	job.Error = patch.Error
	job.UpstreamStatus = patch.UpstreamStatus
	job.RequestID = patch.RequestID
	s.jobs[jobID] = job
	s.jobsMu.Unlock()
	s.publishJob(job)
}

func (s *Server) getImageJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	s.jobsMu.RLock()
	job, ok := s.jobs[jobID]
	s.jobsMu.RUnlock()
	if !ok || job.UserID != currentUser(r).ID {
		writeRawJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "图片任务不存在或已过期", "code": "IMAGE_JOB_NOT_FOUND"}})
		return
	}
	writeRawJSON(w, http.StatusOK, job)
}

func (s *Server) analyzeImageReferences(w http.ResponseWriter, r *http.Request) {
	var request ImageReferenceAnalysisRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Images) == 0 {
		writeFail(w, http.StatusBadRequest, "图片列表不能为空", "VALIDATION_ERROR")
		return
	}
	baseID := request.ContextImageID
	if baseID == "" {
		baseID = inferBaseImageID(request.Prompt, request.Images)
	}
	if baseID == "" {
		baseID = request.Images[0].ID
	}
	references := make([]string, 0, len(request.Images)-1)
	for _, image := range request.Images {
		if image.ID != baseID {
			references = append(references, image.ID)
		}
	}
	writeOK(w, ImageReferenceAnalysisResponse{BaseImageID: baseID, ReferenceImageIDs: references, Reason: "local semantic fallback", Source: "go-local"})
}

func inferBaseImageID(prompt string, images []ImageReferenceCandidate) string {
	placementPattern := regexp.MustCompile(`@(\S+).{0,16}(脚上|身上|桌子上|墙上|里面|中间|背景|放到|放在|放入|添加到|合成到)`)
	matches := placementPattern.FindAllStringSubmatch(prompt, -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1][1]
		for _, image := range images {
			if image.ID == last || image.Name == last {
				return image.ID
			}
		}
	}
	if len(images) > 1 {
		return images[len(images)-1].ID
	}
	return images[0].ID
}

func (s *Server) imageJobWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.ws.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "connected"})
	var userID string
	for {
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			if userID != "" {
				s.unregisterClient(userID, conn)
			}
			return
		}
		switch message["type"] {
		case "auth":
			token, _ := message["token"].(string)
			verifiedUserID, err := s.verifyToken(token)
			if err != nil {
				_ = conn.WriteJSON(map[string]any{"type": "error", "error": map[string]any{"message": "登录状态已失效，请重新登录", "code": "INVALID_TOKEN"}})
				return
			}
			userID = verifiedUserID
			s.registerClient(userID, conn)
			_ = conn.WriteJSON(map[string]any{"type": "authenticated"})
			if jobID, _ := message["jobId"].(string); jobID != "" {
				s.sendJobSnapshot(conn, userID, jobID)
			}
		case "subscribe":
			if jobID, _ := message["jobId"].(string); userID != "" && jobID != "" {
				s.sendJobSnapshot(conn, userID, jobID)
			}
		case "ping":
			_ = conn.WriteJSON(map[string]any{"type": "pong"})
		}
	}
}

func (s *Server) registerClient(userID string, conn *websocket.Conn) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.clients[userID] == nil {
		s.clients[userID] = map[*websocket.Conn]bool{}
	}
	s.clients[userID][conn] = true
}

func (s *Server) unregisterClient(userID string, conn *websocket.Conn) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	delete(s.clients[userID], conn)
}

func (s *Server) publishJob(job ImageJob) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	for conn := range s.clients[job.UserID] {
		_ = conn.WriteJSON(map[string]any{"type": "image-job", "job": job})
	}
}

func (s *Server) sendJobSnapshot(conn *websocket.Conn, userID string, jobID string) {
	s.jobsMu.RLock()
	job, ok := s.jobs[jobID]
	s.jobsMu.RUnlock()
	if !ok || job.UserID != userID {
		_ = conn.WriteJSON(map[string]any{"type": "image-job-error", "jobId": jobID, "error": map[string]any{"message": "图片任务不存在或已过期", "code": "IMAGE_JOB_NOT_FOUND"}})
		return
	}
	_ = conn.WriteJSON(map[string]any{"type": "image-job", "job": job})
}

func (s *Server) createImageExport(w http.ResponseWriter, r *http.Request) {
	var request ImageExportRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Images) == 0 || len(request.Images) > 100 {
		writeFail(w, http.StatusBadRequest, "请选择要导出的图片", "VALIDATION_ERROR")
		return
	}
	var zipBytes bytes.Buffer
	zipWriter := zip.NewWriter(&zipBytes)
	for index, image := range request.Images {
		asset, err := s.store.GetAsset(r.Context(), image.AssetID)
		if err != nil {
			writeFail(w, http.StatusNotFound, "图片资源不存在", "ASSET_NOT_FOUND")
			return
		}
		data, _, err := s.objects.Get(r.Context(), asset.ObjectKey)
		if err != nil {
			writeFail(w, http.StatusNotFound, "图片文件不存在", "ASSET_CONTENT_NOT_FOUND")
			return
		}
		filename := safeFilename(image.Filename)
		if filename == "file" {
			filename = fmt.Sprintf("livart-image-%d%s", index+1, filepath.Ext(asset.OriginalFilename))
		}
		entry, err := zipWriter.Create(filename)
		if err != nil {
			writeFail(w, http.StatusInternalServerError, "创建导出文件失败", "EXPORT_CREATE_FAILED")
			return
		}
		_, _ = entry.Write(data)
	}
	if err := zipWriter.Close(); err != nil {
		writeFail(w, http.StatusInternalServerError, "关闭导出文件失败", "EXPORT_CREATE_FAILED")
		return
	}
	exportID := uuid.NewString()
	filename := safeFilename(request.Filename)
	if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		filename += ".zip"
	}
	expiresAt := time.Now().UTC().Add(s.Config.ExportTTL)
	if err := s.store.SaveExport(r.Context(), ExportFile{ID: exportID, UserID: currentUser(r).ID, Filename: filename, Bytes: zipBytes.Bytes(), ExpiresAt: expiresAt}); err != nil {
		writeFail(w, http.StatusInternalServerError, "保存导出文件失败", "EXPORT_SAVE_FAILED")
		return
	}
	writeOK(w, ExportResponse{ExportID: exportID, Filename: filename, DownloadURL: "/api/exports/" + exportID + "/download", ExpiresAt: expiresAt})
}

func (s *Server) downloadExport(w http.ResponseWriter, r *http.Request) {
	exportFile, err := s.store.GetExport(r.Context(), currentUser(r).ID, chi.URLParam(r, "exportId"))
	if err != nil || time.Now().UTC().After(exportFile.ExpiresAt) {
		writeFail(w, http.StatusNotFound, "导出文件不存在或已过期", "EXPORT_NOT_FOUND")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", exportFile.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(exportFile.Bytes)
}

func (s *Server) adminGetConfig(w http.ResponseWriter, r *http.Request) {
	config, ok, err := s.publicSiteAPIConfig(r.Context())
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取全站配置失败", "ADMIN_CONFIG_READ_FAILED")
		return
	}
	if !ok {
		writeOK[*APIConfigResponse](w, nil)
		return
	}
	writeOK(w, config)
}

func (s *Server) adminSaveConfig(w http.ResponseWriter, r *http.Request) {
	var request APIConfigRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.BaseURL) == "" || strings.TrimSpace(request.APIKey) == "" {
		writeFail(w, http.StatusBadRequest, "Base URL 和 API Key 不能为空", "VALIDATION_ERROR")
		return
	}
	config := s.buildAPIConfig(request.BaseURL, request.APIKey, request.Model, request.ChatModel, request.ImageModels, request.ChatModels, true)
	if !modelInList(config.Model, config.ImageModels) {
		writeFail(w, http.StatusBadRequest, "默认生图模型必须包含在生图模型列表中", "VALIDATION_ERROR")
		return
	}
	if !modelInList(config.ChatModel, config.ChatModels) {
		writeFail(w, http.StatusBadRequest, "默认对话模型必须包含在对话模型列表中", "VALIDATION_ERROR")
		return
	}
	saved, err := s.store.SaveSystemAPIConfig(r.Context(), config)
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "保存全站配置失败", "ADMIN_CONFIG_SAVE_FAILED")
		return
	}
	writeOK(w, saved)
}

func (s *Server) adminSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.store.AdminSummary(r.Context())
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取管理概览失败", "ADMIN_SUMMARY_FAILED")
		return
	}
	writeOK(w, summary)
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.AdminListUsers(r.Context())
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取用户列表失败", "ADMIN_USERS_FAILED")
		return
	}
	writeOK(w, users)
}

func (s *Server) adminCanvases(w http.ResponseWriter, r *http.Request) {
	canvases, err := s.store.AdminListCanvases(r.Context())
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取画布列表失败", "ADMIN_CANVASES_FAILED")
		return
	}
	writeOK(w, canvases)
}

func (s *Server) adminAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.store.AdminListAssets(r.Context())
	if err != nil {
		writeFail(w, http.StatusInternalServerError, "读取资源列表失败", "ADMIN_ASSETS_FAILED")
		return
	}
	writeOK(w, assets)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		writeFail(w, http.StatusBadRequest, "请求 JSON 格式无效", "INVALID_JSON")
		return false
	}
	return true
}

func writeOK[T any](w http.ResponseWriter, data T) { writeOKStatus(w, http.StatusOK, data) }

func writeOKStatus[T any](w http.ResponseWriter, status int, data T) {
	writeRawJSON(w, status, Envelope[T]{Success: true, Data: data})
}

func writeFail(w http.ResponseWriter, status int, message string, code string) {
	writeRawJSON(w, status, Envelope[any]{Success: false, Error: &APIError{Message: message, Code: code}})
}

func writeRawJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writePromptHeaders(w http.ResponseWriter, original, optimized string) {
	if original != "" {
		w.Header().Set("X-Livart-Original-Prompt-B64", base64.StdEncoding.EncodeToString([]byte(original)))
	}
	if optimized != "" {
		w.Header().Set("X-Livart-Optimized-Prompt-B64", base64.StdEncoding.EncodeToString([]byte(optimized)))
	}
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "file"
	}
	value = filepath.Base(value)
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	value = replacer.Replace(value)
	if strings.Trim(value, ". ") == "" {
		return "file"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var errNotFound = errors.New("not found")

func sortCanvases(canvases []CanvasSummary) {
	sort.SliceStable(canvases, func(i, j int) bool {
		return canvases[i].UpdatedAt.After(canvases[j].UpdatedAt)
	})
}
