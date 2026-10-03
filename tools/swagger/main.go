// O programa swagger gera docs/swagger.html (Swagger UI com a especificação embutida) a partir
// do openapi.yaml, que é a fonte do contrato. Uso: make swagger
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const page = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><title>Passarim BFF — API</title>
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/5.17.14/swagger-ui.min.css"></head>
<body><div id="ui"></div>
<script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/5.17.14/swagger-ui-bundle.min.js"></script>
<script>
// Gerado a partir de openapi.yaml (make swagger). Não editar à mão.
const spec = %s;
SwaggerUIBundle({ spec, dom_id: '#ui', docExpansion: 'list', tryItOutEnabled: true });
</script></body></html>
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "swagger:", err)
		os.Exit(1)
	}
}

func run() error {
	doc, err := openapi3.NewLoader().LoadFromFile("openapi.yaml")
	if err != nil {
		return err
	}
	// "Try it out" aponta para o BFF do docker compose (127.0.0.1: evita conflito com IPv6 em localhost).
	doc.Servers = openapi3.Servers{{URL: "http://127.0.0.1:8080", Description: "BFF local via Traefik (docker compose)"}}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	js := strings.ReplaceAll(string(b), "</", `<\/`)                               // nunca fecha o <script> por acidente
	return os.WriteFile("docs/swagger.html", []byte(fmt.Sprintf(page, js)), 0o644) //nolint:gosec // arquivo de documentação público
}
