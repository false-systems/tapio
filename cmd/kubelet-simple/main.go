package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"go.uber.org/zap"
)

var (
	kubeletAddress = flag.String("kubelet-address", "localhost:10250", "Kubelet address")
	logLevel       = flag.String("log-level", "debug", "Log level")
)

func main() {
	flag.Parse()

	// Create logger
	var logger *zap.Logger
	var err error
	if *logLevel == "debug" {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting simple Kubelet API test",
		zap.String("address", *kubeletAddress))

	// Create HTTP client with insecure TLS (for testing)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Test Kubelet endpoints
	endpoints := []string{
		"/healthz",
		"/stats/summary",
		"/pods",
	}

	for _, endpoint := range endpoints {
		url := fmt.Sprintf("https://%s%s", *kubeletAddress, endpoint)
		logger.Info("Testing endpoint", zap.String("url", url))

		resp, err := client.Get(url)
		if err != nil {
			logger.Error("Failed to reach endpoint",
				zap.String("endpoint", endpoint),
				zap.Error(err))
			continue
		}
		defer resp.Body.Close()

		logger.Info("Response received",
			zap.String("endpoint", endpoint),
			zap.Int("status", resp.StatusCode))

		if endpoint == "/healthz" {
			body, _ := io.ReadAll(resp.Body)
			logger.Info("Health check response",
				zap.String("body", string(body)))
		}

		if endpoint == "/stats/summary" && resp.StatusCode == 200 {
			var stats map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&stats); err == nil {
				if node, ok := stats["node"].(map[string]interface{}); ok {
					if nodeName, ok := node["nodeName"].(string); ok {
						logger.Info("Node name from stats",
							zap.String("nodeName", nodeName))
					}
				}
			}
		}
	}

	// Keep running and poll every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Info("Polling Kubelet every 30 seconds... Press Ctrl+C to stop")

	for {
		select {
		case <-ticker.C:
			url := fmt.Sprintf("https://%s/healthz", *kubeletAddress)
			resp, err := client.Get(url)
			if err != nil {
				logger.Error("Health check failed", zap.Error(err))
			} else {
				resp.Body.Close()
				logger.Info("Health check OK",
					zap.Int("status", resp.StatusCode))
			}
		case <-ctx.Done():
			return
		}
	}
}