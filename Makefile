.PHONY: backend web-install web-dev web-build build tidy

backend:
	go run ./cmd/printd

web-install:
	cd web && pnpm install

web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build

build:
	cd web && pnpm build
	go build -o printd ./cmd/printd

tidy:
	go mod tidy
