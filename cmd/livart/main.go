package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"gpt-livart/internal/app"
	"gpt-livart/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	application, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatalf("start livart: %v", err)
	}
	defer application.Close()

	server := &http.Server{Addr: application.HTTPAddr, Handler: application.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("livart Go backend listening on %s", application.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
