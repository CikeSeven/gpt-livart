package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

func TestUserConfigFallsBackToServerDefaultAndCanBeSaved(t *testing.T) {
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

	save := requestJSON(t, server.Handler, http.MethodPut, "/api/user/config", token, map[string]any{
		"baseUrl":   "https://user.example/v1/",
		"apiKey":    "user-key",
		"model":     "gpt-image-2",
		"chatModel": "gpt-5.5",
	})
	if save.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", save.Code, save.Body.String())
	}
	saved := decodeEnvelope[APIConfigResponse](t, save.Body)
	if saved.Data.ServerDefault || saved.Data.BaseURL != "https://user.example/v1" || saved.Data.APIKey != "user-key" {
		t.Fatalf("unexpected saved config: %#v", saved)
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
	if job.Code != http.StatusOK {
		t.Fatalf("job status = %d body=%s", job.Code, job.Body.String())
	}
	var submission ImageJob
	if err := json.NewDecoder(job.Body).Decode(&submission); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if submission.JobID == "" || submission.Status == "" || submission.OriginalPrompt != "画一只猫" {
		t.Fatalf("unexpected job submission: %#v", submission)
	}
}
