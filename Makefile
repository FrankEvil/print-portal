.PHONY: backend web-install web-dev web-build web-typecheck build tidy test test-integration check

backend:
	go run ./cmd/printd

web-install:
	cd web && pnpm install

web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build

web-typecheck:
	cd web && pnpm typecheck

build:
	cd web && pnpm build
	go build -o printd ./cmd/printd

tidy:
	go mod tidy

test:
	go test ./...

test-integration:
	./scripts/integration_test.sh

check: test web-typecheck web-build
