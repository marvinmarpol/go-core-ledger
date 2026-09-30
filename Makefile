# Variables
GO_MAIN := ./cmd/ledger-api
BUILD_DIR := ./dist
EXECUTABLE := $(BUILD_DIR)/myapp
DATABASE_URL ?= postgres://admin:admin123@localhost:5432/core_ledger?sslmode=disable

# Targets
.PHONY: all build wire test test-race test-int lint sqlc migrate-up run clean setup

all: wire test run

build:
	@echo "Building the project..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(EXECUTABLE) $(GO_MAIN)

wire:
	@echo "Running wire for dependency injection..."
	@wire ./...

test:
	@echo "Running tests in verbose mode..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html

test-race:
	@echo "Running tests with race detector..."
	@go test -race ./...

test-int:
	@echo "Running integration tests..."
	@go test -tags integration -count=1 ./...

lint:
	@echo "Running linter..."
	@golangci-lint run

sqlc:
	@echo "Regenerating sqlc queries..."
	@go run -mod=mod github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

migrate-up:
	@echo "Applying database migrations..."
	@go run -mod=mod github.com/pressly/goose/v3/cmd/goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	@echo "Roll back a single database migration from the current version..."
	@go run -mod=mod github.com/pressly/goose/v3/cmd/goose -dir db/migrations postgres "$(DATABASE_URL)" down

run: build
	@echo "Running the executable..."
	@$(EXECUTABLE)

setup:
	@echo "Installing dev tools..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@go install github.com/google/wire/cmd/wire@latest

clean:
	@echo "Cleaning build files..."
	@go clean
	@rm -rf $(BUILD_DIR)