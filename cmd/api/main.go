package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type APIServer struct {
	neo4jDriver neo4j.DriverWithContext
	router      *mux.Router
	server      *http.Server
}

type WhyResponse struct {
	Pod         string          `json:"pod"`
	Namespace   string          `json:"namespace"`
	RootCause   string          `json:"root_cause"`
	Timeline    []TimelineEvent `json:"timeline"`
	Impact      []string        `json:"impact"`
	Suggestions []string        `json:"suggestions"`
}

type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Event     string    `json:"event"`
	Details   string    `json:"details"`
}

type ImpactResponse struct {
	Service       string   `json:"service"`
	Namespace     string   `json:"namespace"`
	AffectedPods  []string `json:"affected_pods"`
	AffectedApps  []string `json:"affected_apps"`
	DownstreamDep []string `json:"downstream_dependencies"`
	Severity      string   `json:"severity"`
}

type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Services  map[string]string `json:"services"`
}

func NewAPIServer() (*APIServer, error) {
	neo4jURI := os.Getenv("NEO4J_URI")
	if neo4jURI == "" {
		neo4jURI = "bolt://localhost:7687"
	}

	neo4jUser := os.Getenv("NEO4J_USER")
	if neo4jUser == "" {
		neo4jUser = "neo4j"
	}

	neo4jPassword := os.Getenv("NEO4J_PASSWORD")
	if neo4jPassword == "" {
		neo4jPassword = "password"
	}

	driver, err := neo4j.NewDriverWithContext(
		neo4jURI,
		neo4j.BasicAuth(neo4jUser, neo4jPassword, ""),
		func(config *neo4j.Config) {
			config.MaxConnectionLifetime = 5 * time.Minute
			config.MaxConnectionPoolSize = 25
			config.ConnectionAcquisitionTimeout = 10 * time.Second
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	router := mux.NewRouter()
	router.Use(loggingMiddleware)
	router.Use(corsMiddleware)

	server := &APIServer{
		neo4jDriver: driver,
		router:      router,
	}

	server.setupRoutes()

	return server, nil
}

func (s *APIServer) setupRoutes() {
	s.router.HandleFunc("/api/v1/why", s.handleWhy).Methods("GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/impact", s.handleImpact).Methods("GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/health", s.handleHealth).Methods("GET", "OPTIONS")
}

func (s *APIServer) handleWhy(w http.ResponseWriter, r *http.Request) {
	pod := r.URL.Query().Get("pod")
	namespace := r.URL.Query().Get("namespace")

	if pod == "" {
		http.Error(w, "pod parameter is required", http.StatusBadRequest)
		return
	}

	if namespace == "" {
		namespace = "default"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := s.queryWhyPodFailed(ctx, pod, namespace)
	if err != nil {
		log.Printf("Error querying why pod failed: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) queryWhyPodFailed(ctx context.Context, pod, namespace string) (*WhyResponse, error) {
	session := s.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH path = (p:Pod {name: $pod, namespace: $namespace})-[*1..5]-(root)
		WHERE NOT (root)-->()
		WITH path, root
		ORDER BY length(path) DESC
		LIMIT 1
		RETURN path, root
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"pod":       pod,
		"namespace": namespace,
	})
	if err != nil {
		return nil, err
	}

	timeline := []TimelineEvent{}
	rootCause := "Unknown"
	suggestions := []string{}

	if result.Next(ctx) {
		record := result.Record()
		if pathValue, ok := record.Get("path"); ok {
			if path, ok := pathValue.(neo4j.Path); ok {
				for i := len(path.Nodes) - 1; i >= 0; i-- {
					node := path.Nodes[i]
					props := node.Props

					event := TimelineEvent{
						Timestamp: time.Now().Add(time.Duration(-i) * time.Minute),
						Event:     fmt.Sprintf("%v event", props["type"]),
						Details:   fmt.Sprintf("%v", props["message"]),
					}
					timeline = append(timeline, event)
				}
			}
		}

		if rootValue, ok := record.Get("root"); ok {
			if rootNode, ok := rootValue.(neo4j.Node); ok {
				if msg, exists := rootNode.Props["message"]; exists {
					rootCause = fmt.Sprintf("%v", msg)
				}
			}
		}
	}

	if strings.Contains(rootCause, "OOMKilled") {
		suggestions = append(suggestions, "Increase memory limits for the pod")
		suggestions = append(suggestions, "Optimize application memory usage")
	} else if strings.Contains(rootCause, "ImagePullBackOff") {
		suggestions = append(suggestions, "Verify image exists in registry")
		suggestions = append(suggestions, "Check image pull secrets")
	}

	return &WhyResponse{
		Pod:         pod,
		Namespace:   namespace,
		RootCause:   rootCause,
		Timeline:    timeline,
		Impact:      []string{},
		Suggestions: suggestions,
	}, nil
}

func (s *APIServer) handleImpact(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	namespace := r.URL.Query().Get("namespace")

	if service == "" {
		http.Error(w, "service parameter is required", http.StatusBadRequest)
		return
	}

	if namespace == "" {
		namespace = "default"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := s.queryServiceImpact(ctx, service, namespace)
	if err != nil {
		log.Printf("Error querying service impact: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) queryServiceImpact(ctx context.Context, service, namespace string) (*ImpactResponse, error) {
	session := s.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH (s:Service {name: $service, namespace: $namespace})-[*1..3]-(affected)
		WHERE affected:Pod OR affected:Service OR affected:Deployment
		RETURN DISTINCT affected.name as name, labels(affected)[0] as type
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"service":   service,
		"namespace": namespace,
	})
	if err != nil {
		return nil, err
	}

	affectedPods := []string{}
	affectedApps := []string{}
	downstreamDep := []string{}

	for result.Next(ctx) {
		record := result.Record()
		name, _ := record.Get("name")
		nodeType, _ := record.Get("type")

		switch nodeType {
		case "Pod":
			affectedPods = append(affectedPods, name.(string))
		case "Deployment":
			affectedApps = append(affectedApps, name.(string))
		case "Service":
			downstreamDep = append(downstreamDep, name.(string))
		}
	}

	severity := "low"
	if len(affectedPods) > 5 {
		severity = "high"
	} else if len(affectedPods) > 2 {
		severity = "medium"
	}

	return &ImpactResponse{
		Service:       service,
		Namespace:     namespace,
		AffectedPods:  affectedPods,
		AffectedApps:  affectedApps,
		DownstreamDep: downstreamDep,
		Severity:      severity,
	}, nil
}

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	services := make(map[string]string)

	var err error
	if s.neo4jDriver != nil {
		err = s.neo4jDriver.VerifyConnectivity(ctx)
	} else {
		err = fmt.Errorf("driver not initialized")
	}
	if err != nil {
		services["neo4j"] = "unhealthy"
	} else {
		services["neo4j"] = "healthy"
	}

	status := "healthy"
	if services["neo4j"] != "healthy" {
		status = "degraded"
	}

	response := HealthResponse{
		Status:    status,
		Timestamp: time.Now(),
		Services:  services,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *APIServer) Start() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	s.server = &http.Server{
		Addr:         ":" + port,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("API Server starting on port %s", port)
	return s.server.ListenAndServe()
}

func (s *APIServer) Stop(ctx context.Context) error {
	log.Println("API Server shutting down...")
	s.neo4jDriver.Close(ctx)
	return s.server.Shutdown(ctx)
}

func main() {
	server, err := NewAPIServer()
	if err != nil {
		log.Fatalf("Failed to create API server: %v", err)
	}

	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Received shutdown signal")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Stop(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
