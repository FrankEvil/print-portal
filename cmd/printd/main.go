package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/liuzhaojun/print-portal/internal/api"
	"github.com/liuzhaojun/print-portal/internal/config"
	"github.com/liuzhaojun/print-portal/internal/convert"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/discovery"
	"github.com/liuzhaojun/print-portal/internal/driver"
	"github.com/liuzhaojun/print-portal/internal/job"
	"github.com/liuzhaojun/print-portal/internal/printer"
	"github.com/liuzhaojun/print-portal/internal/scan"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

func main() {
	cfg := config.Load()
	if err := cfg.EnsureDataDir(); err != nil {
		log.Fatalf("prepare data dir: %v", err)
	}
	if cfg.StaticDir == "" {
		if _, err := os.Stat("web/dist"); err == nil {
			cfg.StaticDir = "web/dist"
		}
	}

	logFile, err := os.OpenFile(filepath.Join(cfg.DataDir, "logs", "printd.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("open log file: %v", err)
	}
	defer logFile.Close()
	logger := log.New(io.MultiWriter(os.Stdout, logFile), "", log.LstdFlags)

	os.Setenv("CUPS_SERVER", cfg.CUPSServer)

	store, err := storage.Open(filepath.Join(cfg.DataDir, "print-portal.db"))
	if err != nil {
		logger.Fatalf("open database: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		logger.Fatalf("migrate database: %v", err)
	}
	if err := store.ResetOrphanJobs(); err != nil {
		logger.Printf("reset orphan jobs: %v", err)
	}

	cupsClient := cups.NewClient(cups.ExecRunner{}, cfg.CUPSServer)
	converter := convert.New(cfg.DataDir)
	jobManager := job.New(store, cupsClient, converter, cfg.DataDir, logger)
	printerService := printer.NewService(store, cupsClient)
	driverService := driver.NewService(store, cupsClient, cfg.DataDir)
	discoveryService := discovery.NewService(store, cupsClient)
	scanService := scan.NewService(store, cfg.DataDir, cups.ExecRunner{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobManager.Start(ctx)

	// Best-effort driver replay/refresh on startup.
	go func() {
		refreshCtx, refreshCancel := context.WithTimeout(ctx, 60*time.Second)
		defer refreshCancel()
		if err := driverService.ReplayInstalled(refreshCtx); err != nil {
			logger.Printf("driver replay: %v", err)
		}
		if err := driverService.Refresh(refreshCtx); err != nil {
			logger.Printf("driver refresh: %v", err)
		}
	}()

	server := api.New(cfg, store, cupsClient, jobManager, printerService, driverService, discoveryService, scanService, logger)
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Printf("shutting down")
		jobManager.Stop()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		cancel()
	}()

	if cfg.AdminToken == "" {
		logger.Printf("WARNING: ADMIN_TOKEN is empty; admin API is open (development only)")
	}
	logger.Printf("printd listening on %s (cups=%s)", cfg.Addr, cfg.CUPSServer)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("listen: %v", err)
	}
}
