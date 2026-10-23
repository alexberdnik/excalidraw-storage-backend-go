.PHONY: build test vet run docker

build:
	go build -trimpath -o bin/server .

test:
	go test ./...

vet:
	go vet ./...

run:
	go run .

docker:
	docker build -t excalidraw-storage-backend-go .
