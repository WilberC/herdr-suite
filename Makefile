.PHONY: build hs test vet fmt

build:
	go build ./...

hs:
	go build -o bin/hs ./cmd/hs

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .
