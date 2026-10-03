package server

import (
	"context"
	"net"
	"net/http"
	"time"
)

// Healthcheck é o subcomando "bff healthcheck": a imagem distroless não tem curl,
// então o próprio binário consulta /healthz. Usa 127.0.0.1 (e não "localhost":
// no container, localhost pode resolver para IPv6 primeiro). Sai 0 se saudável, 1 senão.
func Healthcheck(httpAddr string) int {
	if httpAddr == "" {
		httpAddr = ":8080"
	}
	_, port, err := net.SplitHostPort(httpAddr)
	if err != nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
