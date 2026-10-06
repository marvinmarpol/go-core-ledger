// Package worker runs the ledger background jobs: outbox relay, hold expiry,
// and daily balance verification.
package worker

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-core-ledger/internal/holds"
	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/outbox"
	"go-core-ledger/internal/posting"
	store "go-core-ledger/internal/store/pg"
)

// wallClock is the real-time Clock used in production.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// Run loads config, connects to the database, starts all background jobs, and
// blocks until SIGINT or SIGTERM is received.
func Run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := connectDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	clock := wallClock{}
	st := store.NewStore(pool)
	postSvc := posting.NewService(pool, st, clock)
	holdsSvc := holds.NewService(pool, st, postSvc, clock)
	relay := outbox.NewRelay(st, LogPublisher{}, cfg.RelayBatch)

	var wg sync.WaitGroup

	wg.Add(1)
	go runLoop(ctx, &wg, cfg.RelayInterval, "relay", func(ctx context.Context) error {
		n, err := relay.Process(ctx)
		if err != nil {
			return fmt.Errorf("relay: %w", err)
		}
		if n > 0 {
			log.Printf("[relay] published %d events", n)
		}
		return nil
	})

	wg.Add(1)
	go runLoop(ctx, &wg, cfg.ExpiryInterval, "expiry", func(ctx context.Context) error {
		return expireHolds(ctx, st, holdsSvc, cfg.ExpiryBatch)
	})

	wg.Add(1)
	go runLoop(ctx, &wg, cfg.VerifyInterval, "verify", func(ctx context.Context) error {
		return verifyBalances(ctx, st)
	})

	log.Printf("ledger-worker: started (relay=%s expiry=%s verify=%s)",
		cfg.RelayInterval, cfg.ExpiryInterval, cfg.VerifyInterval)
	wg.Wait()
	log.Printf("ledger-worker: stopped")
	return nil
}

// runLoop ticks at interval and calls fn each tick until ctx is cancelled.
func runLoop(ctx context.Context, wg *sync.WaitGroup, interval time.Duration, name string, fn func(context.Context) error) {
	defer wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := fn(ctx); err != nil {
				log.Printf("[%s] error: %v", name, err)
			}
		}
	}
}

// expireHolds fetches expired holds and transitions each to the expired status.
func expireHolds(ctx context.Context, st *store.Store, holdsSvc *holds.Service, batch int32) error {
	expired, err := st.GetExpiredHolds(ctx, batch)
	if err != nil {
		return fmt.Errorf("fetch expired holds: %w", err)
	}
	for _, h := range expired {
		if _, err := holdsSvc.Expire(ctx, h.ID); err != nil {
			log.Printf("[expiry] hold %s: %v", h.ID, err)
		}
	}
	if len(expired) > 0 {
		log.Printf("[expiry] processed %d expired holds", len(expired))
	}
	return nil
}

// verifyBalances checks the ledger invariant: stored balance == sum of postings.
// Any mismatch is logged as an error; this does not halt the worker.
func verifyBalances(ctx context.Context, st *store.Store) error {
	mismatches, err := st.VerifyAccountBalances(ctx)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if len(mismatches) == 0 {
		log.Printf("[verify] all account balances consistent")
		return nil
	}
	for _, m := range mismatches {
		log.Printf("[verify] INVARIANT VIOLATION account=%s currency=%s stored=%d computed=%d",
			m.AccountID, m.Currency, m.StoredBalance, m.ComputedBalance)
	}
	return nil
}

// connectDB opens and validates a pgxpool connection.
func connectDB(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPass, cfg.DBName,
	)
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	poolCfg.MaxConns = cfg.DBPool
	poolCfg.MaxConnIdleTime = time.Duration(cfg.DBIdle) * time.Second

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

// ledger.Clock is satisfied by wallClock — verify at compile time.
var _ ledger.Clock = wallClock{}
