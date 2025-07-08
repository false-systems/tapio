# Dead simple Makefile
.PHONY: build test clean

build:
	go build ./cmd/tapio

test:
	go test ./...

clean:
	rm -f tapio