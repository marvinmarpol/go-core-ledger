package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config holds all runtime configuration for the ledger API service.
type Config struct {
	ServiceAddress string `env:"SERVICE_ADDRESS" env-default:":8080"`
	DBHost         string `env:"LEDGER_DB_HOST" env-default:"localhost"`
	DBPort         int    `env:"LEDGER_DB_PORT" env-default:"5432"`
	DBUser         string `env:"LEDGER_DB_USER" env-default:"admin"`
	DBPassword     string `env:"LEDGER_DB_PASSWORD" env-default:"admin123"`
	DBName         string `env:"LEDGER_DB_NAME" env-default:"core_ledger"`
	DBPoolSize     int32  `env:"LEDGER_DB_POOL_SIZE" env-default:"10"`
	DBMaxRetries   int    `env:"LEDGER_DB_MAX_RETRIES" env-default:"3"`
	DBRetryDelay   int    `env:"LEDGER_DB_RETRY_DELAY" env-default:"3"`
	DBIdleTimeout  int    `env:"LEDGER_DB_IDLE_TIMEOUT" env-default:"30"`
	DBWriteTimeout int    `env:"LEDGER_DB_WRITE_TIMEOUT" env-default:"30"`
	DBPoolTimeout  int    `env:"LEDGER_DB_POOL_TIMEOUT" env-default:"30"`
}

func loadConfig() (Config, error) {
	var config Config
	err := cleanenv.ReadConfig(".env", &config)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return config, fmt.Errorf("read config file: %w", err)
	}

	if errors.Is(err, os.ErrNotExist) {
		if err := cleanenv.ReadEnv(&config); err != nil {
			return config, fmt.Errorf("read env: %w", err)
		}
	}

	return config, nil
}
