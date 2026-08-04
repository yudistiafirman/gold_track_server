package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gold-track-be/internal/app"
	"gold-track-be/internal/config"
	"gold-track-be/internal/logger"
	"gold-track-be/internal/repository"
	"gold-track-be/internal/service"
)

// tokenBlacklistCleanupInterval drives how often expired token_blacklist
// rows get swept — hourly is frequent enough to keep the table from growing
// unbounded without adding meaningful DB load.
const tokenBlacklistCleanupInterval = time.Hour

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	log := logger.New(cfg.App.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, log)
	if err != nil {
		log.Error("failed to initialize app", "error", err)
		os.Exit(1)
	}
	defer application.Pool.Close()
	log.Info("connected to postgres", "host", cfg.Database.Host, "db", cfg.Database.Name)

	srv := &http.Server{
		Addr:         ":" + cfg.App.Port,
		Handler:      application.Router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info("server starting", "port", cfg.App.Port, "env", cfg.App.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	go runGoldPriceSync(ctx, application.GoldPriceService, cfg.GoldPrice.SyncInterval, log)
	go runTokenBlacklistCleanup(ctx, application.TokenBlacklistRepo, log)

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped gracefully")
}

// runGoldPriceSync drives BE-404: syncs once immediately, then again every
// interval until ctx is cancelled. A failed sync just logs and waits for
// the next tick — the active gold_prices row is left untouched, so reads
// keep serving the last good value (see GoldPriceService.SyncOnce).
func runGoldPriceSync(ctx context.Context, svc service.GoldPriceService, interval time.Duration, log *slog.Logger) {
	if err := svc.SyncOnce(ctx); err != nil {
		log.Error("gold price sync failed", "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := svc.SyncOnce(ctx); err != nil {
				log.Error("gold price sync failed", "error", err)
			}
		}
	}
}

// runTokenBlacklistCleanup periodically deletes token_blacklist rows past
// their own expiry (see TokenBlacklistRepository.DeleteExpired) — once a
// token has expired it's rejected on that basis anyway, so the blacklist row
// is just dead weight the table would otherwise accumulate forever.
func runTokenBlacklistCleanup(ctx context.Context, repo repository.TokenBlacklistRepository, log *slog.Logger) {
	ticker := time.NewTicker(tokenBlacklistCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := repo.DeleteExpired(ctx)
			if err != nil {
				log.Error("token blacklist cleanup failed", "error", err)
				continue
			}
			if deleted > 0 {
				log.Info("token blacklist cleanup", "deleted", deleted)
			}
		}
	}
}
