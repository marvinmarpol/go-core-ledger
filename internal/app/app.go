// Package app wires together the application components and starts the server.
package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-core-ledger/api/gen/ledger/v1/ledgerv1connect"
	"go-core-ledger/internal/handler"
	"go-core-ledger/internal/holds"
	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/posting"
	store "go-core-ledger/internal/store/pg"
)

// wallClock is the realtime Clock implementation used in production.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// Run starts the ledger API server. It blocks until the server exits.
func Run() error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()
	pool, err := connectDB(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()

	h := buildHandler(pool, wallClock{})
	return serveHTTP(ctx, h, config.ServiceAddress)
}

// connectDB opens and validates a pgxpool connection using the provided config.
func connectDB(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName,
	)
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	poolCfg.MaxConns = cfg.DBPoolSize
	poolCfg.MaxConnIdleTime = time.Duration(cfg.DBIdleTimeout) * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open db pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

// buildHandler constructs the full service graph and returns the Connect handler.
// Call this with a test pool in integration tests to exercise wiring without binding a port.
func buildHandler(pool *pgxpool.Pool, clock ledger.Clock) *handler.Handler {
	st := store.NewStore(pool)
	postSvc := posting.NewService(pool, st, clock)
	holdsSvc := holds.NewService(pool, st, postSvc, clock)
	return handler.New(st, postSvc, holdsSvc, clock)
}

// serveHTTP mounts the Connect handler on a chi router and starts the HTTP server.
func serveHTTP(ctx context.Context, h *handler.Handler, addr string) error {
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	path, svcHandler := ledgerv1connect.NewLedgerServiceHandler(h)
	log.Printf("ledger-api: Connect handler mounted at %s", path)
	r.Mount(path, svcHandler)

	log.Printf("ledger-api: listening on %s", addr)

	srv := http.Server{
		Addr:              addr,
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
