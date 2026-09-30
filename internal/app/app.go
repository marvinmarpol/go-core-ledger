// Package app wires together the application components and starts the server.
package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run starts the ledger API server. It blocks until the server exits.
func Run() error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		config.DBHost, config.DBPort, config.DBUser, config.DBPassword, config.DBName,
	)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open db pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)

	if err = InitializeApp(r, pool); err != nil {
		return err
	}

	fmt.Println("running server at port ", config.ServiceAddress)

	srv := http.Server{
		Addr:              config.ServiceAddress,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return fmt.Errorf("serve: %w", srv.Serve(ln))
}
