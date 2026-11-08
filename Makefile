.PHONY: test integration vet build run

test:
	go test -race -count=1 -timeout=120s ./...

integration:
	@test -n "$$TEST_DATABASE_URL" || (echo "Set TEST_DATABASE_URL to a test database"; exit 1)
	go test -race -count=1 -timeout=120s ./...

vet:
	go vet ./...

build:
	go build -o bin/api ./cmd/api

run:
	go run ./cmd/api
