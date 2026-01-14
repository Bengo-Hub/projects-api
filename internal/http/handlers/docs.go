package handlers

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed openapi.yaml
var openapiSpec embed.FS

// DocsHandler serves API documentation.
type DocsHandler struct{}

// NewDocsHandler creates a new docs handler.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// OpenAPISpec serves the OpenAPI specification file.
func (h *DocsHandler) OpenAPISpec(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(openapiSpec, "openapi.yaml")
	if err != nil {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
}

// SwaggerUI serves an HTML page with Swagger UI.
func (h *DocsHandler) SwaggerUI(w http.ResponseWriter, r *http.Request) {
	specURL := strings.TrimSuffix(r.URL.Path, "/swagger") + "/openapi.yaml"

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Projects Service API</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
    <style>
        body { margin: 0; padding: 0; }
        .swagger-ui .topbar { display: none; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
        window.onload = function() {
            SwaggerUIBundle({
                url: "` + specURL + `",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIBundle.SwaggerUIStandalonePreset
                ],
                layout: "BaseLayout"
            });
        };
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
