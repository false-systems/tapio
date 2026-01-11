//go:build ignore

// sykli.go - CI pipeline for Tapio
//
// Local:  sykli
// CI:     GitHub Actions runs sykli
package main

import sykli "github.com/yairfalse/sykli/sdk/go"

func main() {
	s := sykli.New()

	// === STATIC ANALYSIS (parallel) ===
	s.Task("fmt-check").
		Run("gofmt -l . | grep -q . && exit 1 || exit 0").
		Inputs("**/*.go")

	s.Task("vet").
		Run("go vet ./...").
		Inputs("**/*.go", "go.mod", "go.sum")

	s.Task("lint").
		Run("golangci-lint run --timeout 5m").
		Inputs("**/*.go", "go.mod", "go.sum")

	// === TESTS (after static analysis) ===
	s.Task("test").
		Run("go test -timeout 5m ./...").
		After("fmt-check", "vet", "lint").
		Inputs("**/*.go", "go.mod", "go.sum")

	// === BUILD (after tests) ===
	s.Task("build").
		Run("mkdir -p bin && CGO_ENABLED=0 go build -ldflags='-s -w' -o bin/tapio ./cmd/tapio 2>/dev/null || echo 'No main.go yet'").
		After("test")

	s.Emit()
}
