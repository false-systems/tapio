package main

import (
    "fmt"
    "os"
)

var (
    version   = "dev"
    buildDate = "unknown"
    gitCommit = "unknown"
)

func main() {
    if len(os.Args) > 1 && os.Args[1] == "version" {
        fmt.Printf("tapio %s (built %s, commit %s)\n", version, buildDate, gitCommit)
        return
    }
    
    fmt.Println("Tapio - Kubernetes Intelligence Tool")
    fmt.Println("Usage: tapio <command>")
    fmt.Println("Commands:")
    fmt.Println("  version    Show version information")
    fmt.Println("  check      Run health checks (coming soon)")
}