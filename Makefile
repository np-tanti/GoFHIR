.PHONY: run build test vet fmt clean lint

# Build all services
BINS = bin/gateway bin/tls-proxy bin/gateway-auth bin/audit-service bin/fhir-core bin/audit-verify bin/migrate bin/seed bin/secrets

run:
	go run ./cmd/gateway

build: $(BINS)

bin/gateway:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/gateway

bin/tls-proxy:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/tls-proxy

bin/gateway-auth:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/gateway-auth

bin/audit-service:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/audit-service

bin/fhir-core:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/fhir-core

bin/audit-verify:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/audit-verify

bin/migrate:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/migrate

bin/seed:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/seed

bin/secrets:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/secrets

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	go fmt ./...

lint:
	@which staticcheck >/dev/null && staticcheck ./... || echo "install staticcheck: go install honnef.co/go/tools/cmd/staticcheck@latest"

clean:
	rm -rf bin/

# Run all services (for development)
run-all: build
	@echo "Starting all services..."
	@mkdir -p /tmp/gofhir
	@./bin/tls-proxy &
	@./bin/gateway-auth &
	@./bin/audit-service &
	@./bin/fhir-core &
	@echo "All services started. Press Ctrl+C to stop."
	@wait

# Stop all services
stop-all:
	@pkill -f 'bin/(tls-proxy|gateway-auth|audit-service|fhir-core)' || true