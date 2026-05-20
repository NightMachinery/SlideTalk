package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/config"
	"github.com/NightMachinery/SlideTalk/internal/httpserver"
	"github.com/NightMachinery/SlideTalk/internal/realtime"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/slides"
	"github.com/NightMachinery/SlideTalk/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(context.Background(), db.DB); err != nil {
		return err
	}
	authService := auth.NewService(db, cfg.DataDir)
	if err := authService.EnsureAdminToken(context.Background()); err != nil {
		return err
	}
	roomService := rooms.NewService(db)
	slideService, err := slides.NewService(db, filepath.Join(cfg.DataDir, "slides"), cfg.SlideMaxBytes)
	if err != nil {
		return err
	}
	if err := slideService.Cleanup(context.Background(), time.Now().UTC()); err != nil {
		return err
	}
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	go runSlideCleanup(cleanupCtx, slideService)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpserver.New(httpserver.ServerOptions{StaticDir: filepath.Join("web", "dist"), AuthService: authService, RoomService: roomService, Hub: realtime.NewHub(db, authService, roomService), SlideService: slideService}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("slidetalk listening on http://%s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case err := <-errCh:
		return err
	case <-signalCh:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

func runSlideCleanup(ctx context.Context, service *slides.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			if err := service.Cleanup(ctx, at.UTC()); err != nil {
				log.Printf("slide cleanup failed: %v", err)
			}
		}
	}
}
