package main

import (
	"log"
	"time"
)

func main() {
	log.Println("🚀 Tapio Observer starting...")

	for {
		log.Println("📊 Observer running - processing events...")
		time.Sleep(30 * time.Second)
	}
}
