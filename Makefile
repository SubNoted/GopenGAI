build:
	go build ./cmd/api/
	go build ./cmd/cli/

run:
	go run ./cmd/api/

test:
	go test ./... -v

lint:
	go vet ./...

fmt:
	go fmt ./...

tidy:
	go mod tidy

sqlc-generate:
	sqlc generate

clean:
	rm -f api cli
	rm -rf .gopengai/

.PHONY: build run test lint fmt tidy sqlc-generate clean
