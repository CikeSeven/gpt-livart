package api

import (
	"encoding/json"
	"time"
)

type Envelope[T any] struct {
	Success bool      `json:"success"`
	Data    T         `json:"data"`
	Error   *APIError `json:"error,omitempty"`
}

type APIError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

type Config struct {
	JWTSecret            string
	JWTTTLDays           int
	DefaultAPIBaseURL    string
	DefaultAPIKey        string
	DefaultImageModel    string
	DefaultChatModel     string
	AllowedCORSOrigins   []string
	MaxUploadBytes       int64
	ExportTTL            time.Duration
	PublicBaseURL        string
	RequestTimeout       time.Duration
	LocalObjectStorePath string
	StaticDir            string
}

func (c *Config) normalize() {
	if c.JWTSecret == "" {
		c.JWTSecret = "livart-dev-jwt-secret-change-me-at-least-32-bytes"
	}
	if c.JWTTTLDays <= 0 {
		c.JWTTTLDays = 30
	}
	if c.DefaultImageModel == "" {
		c.DefaultImageModel = "gpt-image-2"
	}
	if c.DefaultChatModel == "" {
		c.DefaultChatModel = "gpt-5.5"
	}
	if c.MaxUploadBytes <= 0 {
		c.MaxUploadBytes = 25 << 20
	}
	if c.ExportTTL <= 0 {
		c.ExportTTL = time.Hour
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 10 * time.Minute
	}
	if c.StaticDir == "" {
		c.StaticDir = "./frontend/dist"
	}
}

type AuthUser struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	IsAdmin     bool      `json:"isAdmin"`
	CreatedAt   time.Time `json:"createdAt,omitempty"`
}

type AuthResponse struct {
	User      AuthUser  `json:"user"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type RegisterRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserRecord struct {
	AuthUser
	PasswordHash string
}

type APIConfigRequest struct {
	BaseURL     string   `json:"baseUrl"`
	APIKey      string   `json:"apiKey"`
	Model       string   `json:"model"`
	ChatModel   string   `json:"chatModel"`
	ImageModels []string `json:"imageModels"`
	ChatModels  []string `json:"chatModels"`
}

type APIConfigResponse struct {
	BaseURL         string    `json:"baseUrl"`
	APIKey          string    `json:"apiKey"`
	Model           string    `json:"model"`
	ChatModel       string    `json:"chatModel"`
	ImageModels     []string  `json:"imageModels"`
	ChatModels      []string  `json:"chatModels"`
	TextToImageURL  string    `json:"textToImageUrl"`
	ImageToImageURL string    `json:"imageToImageUrl"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
	ServerDefault   bool      `json:"serverDefault"`
}

type CanvasSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	Revision  int64     `json:"revision"`
}

type CanvasResponse struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	State     json.RawMessage `json:"state"`
	CreatedAt time.Time       `json:"createdAt,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt,omitempty"`
	Revision  int64           `json:"revision"`
	Queued    bool            `json:"queued"`
}

type CreateCanvasRequest struct {
	Title string `json:"title"`
}

type SaveCanvasRequest struct {
	Title          string          `json:"title"`
	State          json.RawMessage `json:"state"`
	ClientRevision *int64          `json:"clientRevision"`
}

type AssetResponse struct {
	ID               string    `json:"id"`
	CanvasID         string    `json:"canvasId,omitempty"`
	UserID           string    `json:"userId,omitempty"`
	URLPath          string    `json:"urlPath"`
	PreviewURLPath   string    `json:"previewUrlPath"`
	ThumbnailURLPath string    `json:"thumbnailUrlPath"`
	OriginalFilename string    `json:"originalFilename,omitempty"`
	MimeType         string    `json:"mimeType"`
	SizeBytes        int64     `json:"sizeBytes"`
	Width            *int      `json:"width,omitempty"`
	Height           *int      `json:"height,omitempty"`
	CreatedAt        time.Time `json:"createdAt,omitempty"`
	ObjectKey        string    `json:"-"`
}

type ImageReferenceCandidate struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Index  int    `json:"index"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type ImageReferenceAnalysisRequest struct {
	Prompt         string                    `json:"prompt"`
	ContextImageID string                    `json:"contextImageId"`
	Images         []ImageReferenceCandidate `json:"images"`
}

type ImageReferenceAnalysisResponse struct {
	BaseImageID       string   `json:"baseImageId"`
	ReferenceImageIDs []string `json:"referenceImageIds"`
	Reason            string   `json:"reason"`
	Source            string   `json:"source"`
}

type ImageJob struct {
	JobID           string          `json:"jobId"`
	Status          string          `json:"status"`
	OriginalPrompt  string          `json:"originalPrompt,omitempty"`
	OptimizedPrompt string          `json:"optimizedPrompt,omitempty"`
	Response        json.RawMessage `json:"response,omitempty"`
	Error           any             `json:"error,omitempty"`
	UpstreamStatus  int             `json:"upstreamStatus,omitempty"`
	RequestID       string          `json:"requestId,omitempty"`
	Attempts        int             `json:"attempts,omitempty"`
	UserID          string          `json:"-"`
}

type ImageExportItemRequest struct {
	AssetID  string `json:"assetId"`
	Filename string `json:"filename"`
}

type ImageExportRequest struct {
	Images   []ImageExportItemRequest `json:"images"`
	Filename string                   `json:"filename"`
}

type ExportResponse struct {
	ExportID    string    `json:"exportId"`
	Filename    string    `json:"filename"`
	DownloadURL string    `json:"downloadUrl"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type ExportFile struct {
	ID        string
	UserID    string
	Filename  string
	Bytes     []byte
	ExpiresAt time.Time
}

type AdminSummary struct {
	UserCount   int `json:"userCount"`
	AdminCount  int `json:"adminCount"`
	CanvasCount int `json:"canvasCount"`
	AssetCount  int `json:"assetCount"`
}

type AdminCanvas struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UserID    string    `json:"userId"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	Revision  int64     `json:"revision"`
}
