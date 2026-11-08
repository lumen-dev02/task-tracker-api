package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lumen-dev02/task-tracker-api/internal/auth"
	"github.com/lumen-dev02/task-tracker-api/internal/config"
	"github.com/lumen-dev02/task-tracker-api/internal/httpapi"
	"github.com/lumen-dev02/task-tracker-api/internal/service"
	"github.com/lumen-dev02/task-tracker-api/internal/store"
	"github.com/lumen-dev02/task-tracker-api/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	migrate := flag.Bool("migrate", false, "apply schema migration and exit")
	flag.Parse()
	cfg, err := config.Load(*migrate)
	if err != nil {
		return err
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	poolCfg.MaxConns = 10
	poolCfg.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return errors.New("create database pool")
	}
	defer pool.Close()
	startup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := pool.Ping(startup); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	if *migrate {
		if err := migrations.Apply(startup, pool); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		slog.Info("schema is up to date")
		return nil
	}
	// Fail clearly instead of serving requests against an uninitialized schema.
	var migrated bool
	if err := pool.QueryRow(startup, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 1)`).Scan(&migrated); err != nil || !migrated {
		return errors.New("schema not initialized; run with -migrate first")
	}
	tokens, err := auth.New(cfg.JWTSecret, time.Hour)
	if err != nil {
		return err
	}
	svc, err := service.New(store.New(pool))
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{Handler: httpapi.New(svc, tokens, pool.Ping, slog.Default()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	slog.Info("listening", "address", listener.Addr().String())
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	stop()
	slog.Info("draining HTTP requests")
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return err
	}
	return nil
}
