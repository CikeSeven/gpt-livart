package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"gpt-livart/internal/api"
	"gpt-livart/internal/assets"
	"gpt-livart/internal/config"
	"gpt-livart/internal/store"
)

type App struct {
	HTTPAddr string
	Handler  http.Handler
	Close    func()
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	db, err := store.NewPostgresStore(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	var objectStore api.ObjectStore
	if strings.TrimSpace(cfg.MinIOEndpoint) != "" && strings.TrimSpace(cfg.MinIOAccessKey) != "" && strings.TrimSpace(cfg.MinIOSecretKey) != "" {
		objectStore, err = assets.NewMinIOStore(ctx, cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOBucket, cfg.MinIOUseSSL)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("connect minio: %w", err)
		}
	} else {
		objectStore = assets.NewLocalStore(cfg.API.LocalObjectStorePath)
	}

	server := api.NewServer(cfg.API, db, objectStore)
	return &App{HTTPAddr: cfg.HTTPAddr, Handler: server.Handler, Close: db.Close}, nil
}
