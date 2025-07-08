# MEGA simple Makefile that matches CI
.PHONY: build test lint ci clean

build:
	go build ./cmd/tapio

test:
	go test -short ./...

lint:
	gofmt -l . | grep -v vendor | tee fmt-issues.txt && test ! -s fmt-issues.txt
	go vet ./...

ci: lint build test
	@echo "✅ CI checks passed!"

clean:
	rm -f tapio fmt-issues.txt