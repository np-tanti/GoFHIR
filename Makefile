.PHONY: run build test vet fmt clean docker

BIN ?= bin/gateway

run:
	go run ./cmd/gateway

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/gateway

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	go fmt ./...

lint:
	@which staticcheck >/dev/null && staticcheck ./... || echo "install staticcheck: go install honnef.co/go/tools/cmd/staticcheck@latest"

audit-verify:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/audit-verify ./cmd/audit-verify

migrate:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/migrate ./cmd/migrate

seed:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/seed ./cmd/seed

clean:
	rm -rf bin/