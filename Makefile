APP := projects-service

.PHONY: run test lint tidy build

run:
	go run ./cmd/api

build:
	CGO_ENABLED=0 go build -o bin/$(APP) ./cmd/api

lint:
	golangci-lint run ./...

test:
	go test ./...

tidy:
	go mod tidy

