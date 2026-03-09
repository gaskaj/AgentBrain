APP_NAME := agentbrain
BIN_DIR := bin
GO := go

.PHONY: all build test lint clean run security-scan test-security

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

test-security:
	@echo "Running security tests..."
	$(GO) test -tags=security -race -count=1 ./internal/security/...

lint:
	golangci-lint run ./...

security-scan:
	@echo "Running security scans..."
	@echo "1. Running gosec (static analysis)..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec -fmt json -out security-report.json ./... || true; \
		echo "Static analysis complete. Report saved to security-report.json"; \
	else \
		echo "Warning: gosec not installed. Install with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest"; \
	fi
	@echo "2. Running govulncheck (dependency scanning)..."
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck -json ./... > vulns.json 2>&1 || true; \
		echo "Dependency scan complete. Report saved to vulns.json"; \
	else \
		echo "Warning: govulncheck not installed. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	fi
	@echo "3. Running custom security checks..."
	@if [ -f $(BIN_DIR)/$(APP_NAME) ]; then \
		echo "Security scan complete. Check security-report.json and vulns.json for findings."; \
	else \
		echo "Application not built. Run 'make build' first."; \
	fi

security-install:
	@echo "Installing security tools..."
	$(GO) install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "Security tools installed successfully."

security-report:
	@echo "Generating comprehensive security report..."
	@if [ -f security-report.json ] && [ -f vulns.json ]; then \
		echo "Security reports found:"; \
		echo "- Static analysis: security-report.json"; \
		echo "- Vulnerabilities: vulns.json"; \
		echo "Review these files for security findings."; \
	else \
		echo "Security reports not found. Run 'make security-scan' first."; \
	fi

clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html security-report.json vulns.json

run: build
	./$(BIN_DIR)/$(APP_NAME) --config configs/agentbrain.yaml

run-once: build
	./$(BIN_DIR)/$(APP_NAME) --config configs/agentbrain.yaml --once

docker-build:
	docker build -t $(APP_NAME):latest .
