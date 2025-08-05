package api

import (
	"embed"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

//go:embed openapi.yaml
var openAPISpec embed.FS

// setupDocsRoutes sets up documentation routes
func (s *Server) setupDocsRoutes() {
	// Serve OpenAPI spec
	s.router.HandleFunc("/api/docs/openapi.yaml", s.handleOpenAPISpec).Methods("GET")

	// Serve Swagger UI
	s.router.PathPrefix("/api/docs/").Handler(http.StripPrefix("/api/docs/", s.swaggerUIHandler()))
}

// handleOpenAPISpec serves the OpenAPI specification
func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	data, err := openAPISpec.ReadFile("openapi.yaml")
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to load API specification")
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Write(data)
}

// swaggerUIHandler returns a handler for Swagger UI
func (s *Server) swaggerUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || r.URL.Path == "/" {
			// Serve Swagger UI HTML
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(swaggerUIHTML))
			return
		}

		// Return 404 for other paths
		http.NotFound(w, r)
	}
}

// swaggerUIHTML is a minimal Swagger UI page
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Tapio API Documentation</title>
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.10.0/swagger-ui.css">
    <style>
        body {
            margin: 0;
            padding: 0;
        }
        .swagger-ui .topbar {
            display: none;
        }
        #swagger-ui {
            max-width: 1460px;
            margin: 0 auto;
            padding: 20px;
        }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.10.0/swagger-ui-bundle.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.10.0/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            SwaggerUIBundle({
                url: "/api/docs/openapi.yaml",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                docExpansion: "list",
                defaultModelsExpandDepth: 1,
                defaultModelExpandDepth: 1,
                filter: true,
                showExtensions: true,
                showCommonExtensions: true,
                tryItOutEnabled: true
            });
        };
    </script>
</body>
</html>`

// AddSwaggerRoute adds swagger documentation route to existing router
func AddSwaggerRoute(router *mux.Router, specPath string) {
	// Serve OpenAPI spec from file
	router.HandleFunc("/api/docs/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, specPath)
	}).Methods("GET")

	// Serve Swagger UI
	router.PathPrefix("/api/docs/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || strings.HasSuffix(r.URL.Path, "/docs") {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(swaggerUIHTML))
			return
		}
		http.NotFound(w, r)
	})
}
