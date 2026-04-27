package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func decodeEnvelope[T any](t *testing.T, body io.Reader) Envelope[T] {
	t.Helper()
	var envelope Envelope[T]
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return envelope
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func authToken(t *testing.T, handler http.Handler) string {
	t.Helper()
	rr := requestJSON(t, handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username":    "tester",
		"password":    "secret123",
		"displayName": "Tester",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", rr.Code, rr.Body.String())
	}
	envelope := decodeEnvelope[AuthResponse](t, rr.Body)
	if !envelope.Success || envelope.Data.Token == "" {
		t.Fatalf("expected auth token, got %#v", envelope)
	}
	return envelope.Data.Token
}

func TestAuthRegisterLoginAndMeUseLivartEnvelope(t *testing.T) {
	server := NewTestServer(t)
	token := authToken(t, server.Handler)

	me := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	server.Handler.ServeHTTP(me, req)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", me.Code, me.Body.String())
	}
	meEnvelope := decodeEnvelope[AuthUser](t, me.Body)
	if !meEnvelope.Success || meEnvelope.Data.Username != "tester" || meEnvelope.Data.DisplayName != "Tester" {
		t.Fatalf("unexpected me envelope: %#v", meEnvelope)
	}

	login := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "tester",
		"password": "secret123",
	})
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	loginEnvelope := decodeEnvelope[AuthResponse](t, login.Body)
	if !loginEnvelope.Success || loginEnvelope.Data.Token == "" || loginEnvelope.Data.User.ID == "" {
		t.Fatalf("unexpected login envelope: %#v", loginEnvelope)
	}
}

func TestFirstRegisteredUserIsAdminAndCanReadAdminSummary(t *testing.T) {
	server := NewTestServer(t)

	adminRegister := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "first-admin",
		"password": "secret123",
	})
	if adminRegister.Code != http.StatusOK {
		t.Fatalf("admin register status = %d body=%s", adminRegister.Code, adminRegister.Body.String())
	}
	adminSession := decodeEnvelope[AuthResponse](t, adminRegister.Body)
	if !adminSession.Data.User.IsAdmin {
		t.Fatalf("expected first registered user to be admin: %#v", adminSession.Data.User)
	}

	userRegister := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "normal-user",
		"password": "secret123",
	})
	if userRegister.Code != http.StatusOK {
		t.Fatalf("user register status = %d body=%s", userRegister.Code, userRegister.Body.String())
	}
	userSession := decodeEnvelope[AuthResponse](t, userRegister.Body)
	if userSession.Data.User.IsAdmin {
		t.Fatalf("expected second registered user to be normal user: %#v", userSession.Data.User)
	}

	forbidden := httptest.NewRecorder()
	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/admin/summary", nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+userSession.Data.Token)
	server.Handler.ServeHTTP(forbidden, forbiddenReq)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected normal user forbidden, status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	allowed := httptest.NewRecorder()
	allowedReq := httptest.NewRequest(http.MethodGet, "/api/admin/summary", nil)
	allowedReq.Header.Set("Authorization", "Bearer "+adminSession.Data.Token)
	server.Handler.ServeHTTP(allowed, allowedReq)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected admin summary, status=%d body=%s", allowed.Code, allowed.Body.String())
	}
	summary := decodeEnvelope[AdminSummary](t, allowed.Body)
	if summary.Data.UserCount != 2 || summary.Data.AdminCount != 1 {
		t.Fatalf("unexpected admin summary: %#v", summary.Data)
	}

	assets := httptest.NewRecorder()
	assetsReq := httptest.NewRequest(http.MethodGet, "/api/admin/assets", nil)
	assetsReq.Header.Set("Authorization", "Bearer "+adminSession.Data.Token)
	server.Handler.ServeHTTP(assets, assetsReq)
	if assets.Code != http.StatusOK || !strings.Contains(assets.Body.String(), `"data":[]`) {
		t.Fatalf("expected empty admin assets to include data array, status=%d body=%s", assets.Code, assets.Body.String())
	}

	canvases := httptest.NewRecorder()
	canvasesReq := httptest.NewRequest(http.MethodGet, "/api/admin/canvases", nil)
	canvasesReq.Header.Set("Authorization", "Bearer "+adminSession.Data.Token)
	server.Handler.ServeHTTP(canvases, canvasesReq)
	if canvases.Code != http.StatusOK || !strings.Contains(canvases.Body.String(), `"data":[]`) {
		t.Fatalf("expected empty admin canvases to include data array, status=%d body=%s", canvases.Code, canvases.Body.String())
	}
}

func TestUserConfigFallsBackToServerDefault(t *testing.T) {
	server := NewTestServer(t)
	server.Config.DefaultAPIBaseURL = "https://gateway.example/v1"
	server.Config.DefaultAPIKey = "server-key"
	token := authToken(t, server.Handler)

	getDefault := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/user/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	server.Handler.ServeHTTP(getDefault, req)
	envelope := decodeEnvelope[APIConfigResponse](t, getDefault.Body)
	if !envelope.Success || !envelope.Data.ServerDefault || envelope.Data.APIKey != "" || envelope.Data.BaseURL != "https://gateway.example/v1" {
		t.Fatalf("unexpected default config: %#v", envelope)
	}
}

func TestPublicSiteUsesAdminManagedGlobalAPIConfig(t *testing.T) {
	server := NewTestServer(t)

	adminRegister := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "site-admin",
		"password": "secret123",
	})
	adminSession := decodeEnvelope[AuthResponse](t, adminRegister.Body)
	adminToken := adminSession.Data.Token

	userRegister := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "public-user",
		"password": "secret123",
	})
	userSession := decodeEnvelope[AuthResponse](t, userRegister.Body)
	userToken := userSession.Data.Token

	userSave := requestJSON(t, server.Handler, http.MethodPut, "/api/user/config", userToken, map[string]any{
		"baseUrl":   "https://user.example/v1",
		"apiKey":    "user-key",
		"model":     "gpt-image-2",
		"chatModel": "gpt-5.5",
	})
	if userSave.Code != http.StatusForbidden {
		t.Fatalf("expected user config save forbidden, status=%d body=%s", userSave.Code, userSave.Body.String())
	}

	adminSave := requestJSON(t, server.Handler, http.MethodPut, "/api/admin/config", adminToken, map[string]any{
		"baseUrl":   "https://public-gateway.example/v1/",
		"apiKey":    "global-key",
		"model":     "gpt-image-2",
		"chatModel": "gpt-5.5",
	})
	if adminSave.Code != http.StatusOK {
		t.Fatalf("admin config save status=%d body=%s", adminSave.Code, adminSave.Body.String())
	}
	adminConfig := decodeEnvelope[APIConfigResponse](t, adminSave.Body)
	if adminConfig.Data.BaseURL != "https://public-gateway.example/v1" || adminConfig.Data.APIKey != "global-key" || !adminConfig.Data.ServerDefault {
		t.Fatalf("unexpected admin saved config: %#v", adminConfig.Data)
	}

	userLoad := httptest.NewRecorder()
	userLoadReq := httptest.NewRequest(http.MethodGet, "/api/user/config", nil)
	userLoadReq.Header.Set("Authorization", "Bearer "+userToken)
	server.Handler.ServeHTTP(userLoad, userLoadReq)
	if userLoad.Code != http.StatusOK {
		t.Fatalf("user config load status=%d body=%s", userLoad.Code, userLoad.Body.String())
	}
	publicConfig := decodeEnvelope[APIConfigResponse](t, userLoad.Body)
	if publicConfig.Data.BaseURL != "https://public-gateway.example/v1" || publicConfig.Data.APIKey != "" || !publicConfig.Data.ServerDefault {
		t.Fatalf("expected sanitized global config for user: %#v", publicConfig.Data)
	}
}

func TestAdminConfigPersistsModelListsForUsers(t *testing.T) {
	server := NewTestServer(t)

	adminRegister := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "model-admin",
		"password": "secret123",
	})
	adminSession := decodeEnvelope[AuthResponse](t, adminRegister.Body)
	adminToken := adminSession.Data.Token

	userRegister := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "model-user",
		"password": "secret123",
	})
	userSession := decodeEnvelope[AuthResponse](t, userRegister.Body)
	userToken := userSession.Data.Token

	adminSave := requestJSON(t, server.Handler, http.MethodPut, "/api/admin/config", adminToken, map[string]any{
		"baseUrl":     "https://public-gateway.example/v1/",
		"apiKey":      "global-key",
		"model":       "gpt-image-2",
		"chatModel":   "gpt-5.5",
		"imageModels": []string{"gpt-image-2", "gpt-image-1"},
		"chatModels":  []string{"gpt-5.5", "gpt-5.4"},
	})
	if adminSave.Code != http.StatusOK {
		t.Fatalf("admin config save status=%d body=%s", adminSave.Code, adminSave.Body.String())
	}
	adminConfig := decodeEnvelope[APIConfigResponse](t, adminSave.Body)
	if strings.Join(adminConfig.Data.ImageModels, ",") != "gpt-image-2,gpt-image-1" || strings.Join(adminConfig.Data.ChatModels, ",") != "gpt-5.5,gpt-5.4" {
		t.Fatalf("expected saved model lists, got %#v", adminConfig.Data)
	}

	userLoad := httptest.NewRecorder()
	userLoadReq := httptest.NewRequest(http.MethodGet, "/api/user/config", nil)
	userLoadReq.Header.Set("Authorization", "Bearer "+userToken)
	server.Handler.ServeHTTP(userLoad, userLoadReq)
	if userLoad.Code != http.StatusOK {
		t.Fatalf("user config load status=%d body=%s", userLoad.Code, userLoad.Body.String())
	}
	publicConfig := decodeEnvelope[APIConfigResponse](t, userLoad.Body)
	if publicConfig.Data.APIKey != "" {
		t.Fatalf("expected user config to hide api key, got %#v", publicConfig.Data)
	}
	if strings.Join(publicConfig.Data.ImageModels, ",") != "gpt-image-2,gpt-image-1" || strings.Join(publicConfig.Data.ChatModels, ",") != "gpt-5.5,gpt-5.4" {
		t.Fatalf("expected public model lists, got %#v", publicConfig.Data)
	}
}

func TestAdminConfigRejectsDefaultModelOutsideModelLists(t *testing.T) {
	server := NewTestServer(t)

	adminRegister := requestJSON(t, server.Handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "invalid-model-admin",
		"password": "secret123",
	})
	adminSession := decodeEnvelope[AuthResponse](t, adminRegister.Body)

	adminSave := requestJSON(t, server.Handler, http.MethodPut, "/api/admin/config", adminSession.Data.Token, map[string]any{
		"baseUrl":     "https://public-gateway.example/v1/",
		"apiKey":      "global-key",
		"model":       "gpt-image-2",
		"chatModel":   "gpt-5.5",
		"imageModels": []string{"gpt-image-1"},
		"chatModels":  []string{"gpt-5.5"},
	})
	if adminSave.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid image model list rejected, status=%d body=%s", adminSave.Code, adminSave.Body.String())
	}
}

func TestCanvasProjectFlowPersistsStateAndRevision(t *testing.T) {
	server := NewTestServer(t)
	token := authToken(t, server.Handler)

	create := requestJSON(t, server.Handler, http.MethodPost, "/api/canvases", token, map[string]any{"title": "Demo"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	created := decodeEnvelope[CanvasResponse](t, create.Body)
	if created.Data.ID == "" || created.Data.Title != "Demo" || created.Data.Revision != 0 {
		t.Fatalf("unexpected created canvas: %#v", created)
	}

	save := requestJSON(t, server.Handler, http.MethodPut, "/api/canvases/"+created.Data.ID, token, map[string]any{
		"title":          "Renamed",
		"clientRevision": 0,
		"state": map[string]any{
			"items":       []any{},
			"messages":    []any{},
			"selectedIds": []any{},
		},
	})
	if save.Code != http.StatusAccepted {
		t.Fatalf("save status = %d body=%s", save.Code, save.Body.String())
	}
	saved := decodeEnvelope[CanvasResponse](t, save.Body)
	if !saved.Data.Queued || saved.Data.Revision != 1 || saved.Data.Title != "Renamed" {
		t.Fatalf("unexpected saved canvas: %#v", saved)
	}

	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/canvases", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	server.Handler.ServeHTTP(list, req)
	listed := decodeEnvelope[[]CanvasSummary](t, list.Body)
	if len(listed.Data) != 1 || listed.Data[0].Revision != 1 || listed.Data[0].Title != "Renamed" {
		t.Fatalf("unexpected canvas list: %#v", listed)
	}

	deleteReq := httptest.NewRecorder()
	deleteHTTPReq := httptest.NewRequest(http.MethodDelete, "/api/canvases/"+created.Data.ID, nil)
	deleteHTTPReq.Header.Set("Authorization", "Bearer "+token)
	server.Handler.ServeHTTP(deleteReq, deleteHTTPReq)
	if deleteReq.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteReq.Code, deleteReq.Body.String())
	}

	getDeleted := httptest.NewRecorder()
	getDeletedReq := httptest.NewRequest(http.MethodGet, "/api/canvases/"+created.Data.ID, nil)
	getDeletedReq.Header.Set("Authorization", "Bearer "+token)
	server.Handler.ServeHTTP(getDeleted, getDeletedReq)
	if getDeleted.Code != http.StatusNotFound {
		t.Fatalf("expected deleted canvas not found, status=%d body=%s", getDeleted.Code, getDeleted.Body.String())
	}
}

func TestAssetUploadAndExportZip(t *testing.T) {
	server := NewTestServer(t)
	token := authToken(t, server.Handler)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sample.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("fake-image"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/assets", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	upload := httptest.NewRecorder()
	server.Handler.ServeHTTP(upload, req)
	if upload.Code != http.StatusOK {
		t.Fatalf("upload status = %d body=%s", upload.Code, upload.Body.String())
	}
	asset := decodeEnvelope[AssetResponse](t, upload.Body)
	if asset.Data.ID == "" || !strings.Contains(asset.Data.URLPath, "/api/assets/") || asset.Data.PreviewURLPath == "" {
		t.Fatalf("unexpected asset response: %#v", asset)
	}

	exportReq := requestJSON(t, server.Handler, http.MethodPost, "/api/exports/images", token, map[string]any{
		"filename": "delivery.zip",
		"images": []map[string]any{{
			"assetId":  asset.Data.ID,
			"filename": "sample.png",
		}},
	})
	if exportReq.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", exportReq.Code, exportReq.Body.String())
	}
	exportEnvelope := decodeEnvelope[ExportResponse](t, exportReq.Body)
	if exportEnvelope.Data.ExportID == "" || exportEnvelope.Data.DownloadURL == "" || time.Until(exportEnvelope.Data.ExpiresAt) <= 0 {
		t.Fatalf("unexpected export response: %#v", exportEnvelope)
	}
}

func TestImageReferenceAnalysisAndImageJobs(t *testing.T) {
	server := NewTestServer(t)
	server.Config.DefaultAPIBaseURL = "https://gateway.example/v1"
	server.Config.DefaultAPIKey = "server-key"
	token := authToken(t, server.Handler)

	analysis := requestJSON(t, server.Handler, http.MethodPost, "/api/image-references/analyze", token, map[string]any{
		"prompt":         "把 @shoe 放到 @person 脚上",
		"contextImageId": "person",
		"images": []map[string]any{
			{"id": "shoe", "name": "鞋子", "index": 1, "width": 400, "height": 400},
			{"id": "person", "name": "人物", "index": 2, "width": 800, "height": 1200},
		},
	})
	if analysis.Code != http.StatusOK {
		t.Fatalf("analysis status = %d body=%s", analysis.Code, analysis.Body.String())
	}
	analysisEnvelope := decodeEnvelope[ImageReferenceAnalysisResponse](t, analysis.Body)
	if analysisEnvelope.Data.BaseImageID != "person" || len(analysisEnvelope.Data.ReferenceImageIDs) != 1 || analysisEnvelope.Data.ReferenceImageIDs[0] != "shoe" {
		t.Fatalf("unexpected analysis: %#v", analysisEnvelope)
	}

	job := requestJSON(t, server.Handler, http.MethodPost, "/api/image-jobs/generations", token, map[string]any{
		"model":  "gpt-image-2",
		"prompt": "画一只猫",
	})
	if job.Code != http.StatusAccepted {
		t.Fatalf("job status = %d body=%s", job.Code, job.Body.String())
	}
	var submission ImageJob
	if err := json.NewDecoder(job.Body).Decode(&submission); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if submission.JobID == "" || submission.Status != "pending" || submission.OriginalPrompt != "画一只猫" {
		t.Fatalf("unexpected job submission: %#v", submission)
	}
}

func TestImageGenerationJobCallsUpstreamAndStoresResponse(t *testing.T) {
	var upstreamAuth string
	var upstreamPrompt string
	called := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		upstreamAuth = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		upstreamPrompt, _ = payload["prompt"].(string)
		called <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"image-data"}]}`))
	}))
	defer upstream.Close()

	server := NewTestServer(t)
	server.Config.DefaultAPIBaseURL = upstream.URL
	server.Config.DefaultAPIKey = "server-key"
	token := authToken(t, server.Handler)

	jobResponse := requestJSON(t, server.Handler, http.MethodPost, "/api/image-jobs/generations", token, map[string]any{
		"model":  "gpt-image-2",
		"prompt": "画一只猫",
	})
	if jobResponse.Code != http.StatusAccepted {
		t.Fatalf("job submit status=%d body=%s", jobResponse.Code, jobResponse.Body.String())
	}
	var submitted ImageJob
	if err := json.NewDecoder(jobResponse.Body).Decode(&submitted); err != nil {
		t.Fatalf("decode submitted job: %v", err)
	}
	if submitted.JobID == "" || submitted.Status != "pending" {
		t.Fatalf("expected pending job submission, got %#v", submitted)
	}

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was not called")
	}
	if upstreamAuth != "Bearer server-key" || upstreamPrompt != "画一只猫" {
		t.Fatalf("unexpected upstream request auth=%q prompt=%q", upstreamAuth, upstreamPrompt)
	}

	var completed ImageJob
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		status := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/image-jobs/"+submitted.JobID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		server.Handler.ServeHTTP(status, req)
		if status.Code != http.StatusOK {
			t.Fatalf("job status code=%d body=%s", status.Code, status.Body.String())
		}
		if err := json.NewDecoder(status.Body).Decode(&completed); err != nil {
			t.Fatalf("decode completed job: %v", err)
		}
		if completed.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if completed.Status != "completed" || !strings.Contains(string(completed.Response), "image-data") || completed.UpstreamStatus != http.StatusOK {
		t.Fatalf("expected completed job with upstream response, got %#v response=%s", completed, string(completed.Response))
	}
}

func TestStaticFrontendServesIndexAndSPAFallback(t *testing.T) {
	staticDir := t.TempDir()
	indexPath := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(indexPath, []byte(`<!doctype html><title>livart app</title><div id="root"></div>`), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	server := NewServer(Config{JWTSecret: "test-secret-at-least-32-bytes", StaticDir: staticDir}, newMemoryStore(), newMemoryObjectStore())

	root := httptest.NewRecorder()
	server.Handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), "livart app") {
		t.Fatalf("expected index at root, status=%d body=%s", root.Code, root.Body.String())
	}

	fallback := httptest.NewRecorder()
	server.Handler.ServeHTTP(fallback, httptest.NewRequest(http.MethodGet, "/projects/demo", nil))
	if fallback.Code != http.StatusOK || !strings.Contains(fallback.Body.String(), "livart app") {
		t.Fatalf("expected SPA fallback, status=%d body=%s", fallback.Code, fallback.Body.String())
	}
}
