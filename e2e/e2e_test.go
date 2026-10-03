//go:build e2e

// Teste ponta a ponta: BFF + catalog REAL + Postgres + Redis, tudo em containers.
// Só roda local (make e2e): o catalog só existe como código-fonte vizinho
// (PASSARIM_CATALOG_DIR), então este teste NÃO roda no CI do BFF.
package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func catalogDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("PASSARIM_CATALOG_DIR")
	if dir == "" {
		dir = "../../passarim-catalog"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(abs, "Dockerfile")); err != nil {
		t.Skipf("catalog não encontrado em %s (defina PASSARIM_CATALOG_DIR)", abs)
	}
	return abs
}

func start(t *testing.T, req testcontainers.ContainerRequest) testcontainers.Container {
	t.Helper()
	c, err := testcontainers.GenericContainer(context.Background(), testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("subir %v: %v", req.Name, err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	return c
}

type reply struct {
	status  int
	header  http.Header
	body    map[string]any
	rawBody string
}

func get(t *testing.T, base, path string) reply {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return reply{status: resp.StatusCode, header: resp.Header, body: body, rawBody: string(raw)}
}

func TestE2E_BFFWithRealCatalog(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: precisa de Docker")
	}
	ctx := context.Background()
	net, err := network.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = net.Remove(context.Background()) })
	nets, nname := []string{net.Name}, net.Name
	_ = nname

	start(t, testcontainers.ContainerRequest{
		Name: "e2e-postgres", Image: "postgres:18-alpine", Networks: nets, NetworkAliases: map[string][]string{net.Name: {"postgres"}},
		Env:        map[string]string{"POSTGRES_USER": "p", "POSTGRES_PASSWORD": "p", "POSTGRES_DB": "p"},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60 * time.Second),
	})
	dbURL := "postgres://p:p@postgres:5432/p?sslmode=disable"
	catalogReq := testcontainers.ContainerRequest{
		Name: "e2e-catalog", Networks: nets, NetworkAliases: map[string][]string{net.Name: {"catalog"}},
		FromDockerfile: testcontainers.FromDockerfile{Context: catalogDir(t), Dockerfile: "Dockerfile"},
		Env:            map[string]string{"DATABASE_URL": dbURL},
		WaitingFor:     wait.ForLog("catalog-api no ar").WithStartupTimeout(120 * time.Second),
	}
	catalog := start(t, catalogReq)
	// seed: roda uma vez e termina (carrega as aves curadas).
	start(t, testcontainers.ContainerRequest{
		Name: "e2e-seed", Networks: nets,
		FromDockerfile: testcontainers.FromDockerfile{Context: catalogDir(t), Dockerfile: "Dockerfile"},
		Entrypoint:     []string{"/usr/local/bin/seed", "-dir", "/content/species"},
		Env:            map[string]string{"DATABASE_URL": dbURL},
		WaitingFor:     wait.ForExit().WithExitTimeout(60 * time.Second),
	})
	start(t, testcontainers.ContainerRequest{
		Name: "e2e-redis", Image: "redis:8-alpine", Networks: nets, NetworkAliases: map[string][]string{net.Name: {"redis"}},
		WaitingFor: wait.ForLog("Ready to accept connections"),
	})
	bff := start(t, testcontainers.ContainerRequest{
		Name: "e2e-bff", Networks: nets, ExposedPorts: []string{"8080/tcp"},
		FromDockerfile: testcontainers.FromDockerfile{Context: "..", Dockerfile: "Dockerfile"},
		Env: map[string]string{
			"CATALOG_ADDR": "catalog:50051", "REDIS_URL": "redis://redis:6379",
			"MEDIA_BASE_URL": "http://media.test/bucket", "CACHE_FRESH_TTL": "1s", // frescor curto para provar o stale
		},
		WaitingFor: wait.ForHTTP("/readyz").WithPort("8080/tcp").WithStartupTimeout(90 * time.Second),
	})
	host, _ := bff.Host(ctx)
	port, _ := bff.MappedPort(ctx, "8080/tcp")
	base := "http://" + host + ":" + port.Port()

	t.Run("lista", func(t *testing.T) {
		r := get(t, base, "/v1/species?limit=5")
		items, _ := r.body["items"].([]any)
		if r.status != 200 || len(items) == 0 || r.header.Get("X-Cache") != "miss" || r.header.Get("X-Request-Id") == "" {
			t.Fatalf("lista: %d %v %s", r.status, r.header, r.rawBody)
		}
		if r2 := get(t, base, "/v1/species?limit=5"); r2.header.Get("X-Cache") != "hit" && r2.header.Get("X-Cache") != "miss" {
			t.Fatalf("X-Cache: %q", r2.header.Get("X-Cache"))
		}
	})
	t.Run("detalhe", func(t *testing.T) {
		r := get(t, base, "/v1/species/turdus-rufiventris")
		if r.status != 200 || r.body["scientificName"] != "Turdus rufiventris" || r.body["whereToFind"] == nil {
			t.Fatalf("detalhe: %d %s", r.status, r.rawBody)
		}
	})
	t.Run("404", func(t *testing.T) {
		r := get(t, base, "/v1/species/nao-existe")
		if r.status != 404 || r.body["code"] != "SPECIES_NOT_FOUND" || r.header.Get("Content-Type") != "application/problem+json" {
			t.Fatalf("404: %d %s", r.status, r.rawBody)
		}
	})
	t.Run("filtros", func(t *testing.T) {
		r := get(t, base, "/v1/filters")
		if biomes, _ := r.body["biomes"].([]any); r.status != 200 || len(biomes) == 0 {
			t.Fatalf("filtros: %d %s", r.status, r.rawBody)
		}
	})
	t.Run("catalog parado: stale para o que já foi visto, 503 para o resto", func(t *testing.T) {
		_ = get(t, base, "/v1/species/turdus-rufiventris") // garante que está no cache
		time.Sleep(2 * time.Second)                        // passa do frescor de 1s
		if err := catalog.Stop(ctx, nil); err != nil {
			t.Fatal(err)
		}
		stale := get(t, base, "/v1/species/turdus-rufiventris")
		if stale.status != 200 || stale.header.Get("X-Cache") != "stale" {
			t.Fatalf("esperava 200 stale: %d %q %s", stale.status, stale.header.Get("X-Cache"), stale.rawBody)
		}
		unseen := get(t, base, "/v1/species/harpia-harpyja")
		if unseen.status != 503 || unseen.body["code"] != "SERVICE_UNAVAILABLE" || unseen.header.Get("Content-Type") != "application/problem+json" {
			t.Fatalf("esperava 503 problem+json: %d %s", unseen.status, unseen.rawBody)
		}
	})
}
