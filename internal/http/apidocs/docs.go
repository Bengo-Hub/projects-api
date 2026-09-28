// Package apidocs serves the OpenAPI description of projects-api and a Swagger UI page for it at
// /v1/docs/ (the root path redirects there).
package apidocs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var spec []byte

// Spec returns the embedded OpenAPI document.
func Spec() []byte { return spec }

const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Projects API</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({ url: "openapi.yaml", dom_id: "#swagger" });</script>
</body>
</html>`

// Handler serves the Swagger UI at its root and the spec at openapi.yaml. Mount it under
// /v1/docs with the prefix stripped.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	return mux
}
