package worker

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config holds runtime configuration for the ledger worker process.
type Config struct {
	DBHost    string `env:"LEDGER_DB_HOST"     env-default:"localhost"`
	DBPort    int    `env:"LEDGER_DB_PORT"     env-default:"5432"`
	DBUser    string `env:"LEDGER_DB_USER"     env-default:"admin"`
	DBPass    string `env:"LEDGER_DB_PASSWORD" env-default:"admin123"`
	DBName    string `env:"LEDGER_DB_NAME"     env-default:"core_ledger"`
	DBPool    int32  `env:"LEDGER_DB_POOL_SIZE" env-default:"5"`
	DBIdle    int    `env:"LEDGER_DB_IDLE_TIMEOUT" env-default:"30"`

	RelayInterval  time.Duration `env:"WORKER_RELAY_INTERVAL"  env-default:"5s"`
	RelayBatch     int32         `env:"WORKER_RELAY_BATCH"     env-default:"100"`
	ExpiryInterval time.Duration `env:"WORKER_EXPIRY_INTERVAL" env-default:"30s"`
	ExpiryBatch    int32         `env:"WORKER_EXPIRY_BATCH"    env-default:"50"`
	VerifyInterval time.Duration `env:"WORKER_VERIFY_INTERVAL" env-default:"24h"`
}

func loadConfig() (Config, error) {
	var cfg Config
	err := cleanenv.ReadConfig(".env", &cfg)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, fmt.Errorf("read config file: %w", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return cfg, fmt.Errorf("read env: %w", err)
		}
	}
	return cfg, nil
}
