.PHONY: build
build:
	go build

test:
	go test -v ./...

release:
	goreleaser release --clean
