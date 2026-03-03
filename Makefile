APP_NAME := agentbrain
BIN_DIR := bin
GO := go

.PHONY: all build test lint clean run

all: build

build:
	$(GO) build -o $(BIN_DIR)/$(APP_NAME) ./cmd/agentbrain

test:
	$(GO) test -race -count=1 ./...

test-cover:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

test-integration:
	$(GO) test -race -tags=integration -count=1 ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html

run: build
	./$(BIN_DIR)/$(APP_NAME) --config configs/agentbrain.yaml

run-once: build
	./$(BIN_DIR)/$(APP_NAME) --config configs/agentbrain.yaml --once

docker-build:
	docker build -t $(APP_NAME):latest .
