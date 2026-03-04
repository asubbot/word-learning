.PHONY: help fmt test vet lint coverage coverage-html check

help:
	@echo "Available commands:"
	@echo "  make fmt    - Format Go code"
	@echo "  make test   - Run tests"
	@echo "  make vet    - Run go vet"
	@echo "  make lint   - Run golangci-lint (if installed; includes complexity)"
	@echo "  make coverage     - Print coverage summary"
	@echo "  make coverage-html - Build HTML coverage report"
	@echo "  make check  - Run fmt + vet + lint + coverage"

fmt:
	go fmt ./...

test:
	go test ./...

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed."; \
		echo "Install: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run ./...

coverage:
	go test ./... -coverpkg=./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out

coverage-html:
	go test ./... -coverpkg=./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

check: fmt vet lint coverage
