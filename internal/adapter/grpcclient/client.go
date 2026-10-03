// Package grpcclient implementa usecase.CatalogReader falando gRPC com o
// catalog. Traduz proto → domínio e código gRPC → domain.Kind; não decide
// retry, cache nem breaker (isso é das camadas de fora).
package grpcclient

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	catalogv1 "github.com/velosobr/passarim-proto/gen/go/passarim/catalog/v1"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

const requestIDHeader = "x-request-id"

type Config struct {
	Addr   string
	TLS    TLS
	Logger *slog.Logger
}

type Client struct {
	conn *grpc.ClientConn
	rpc  catalogv1.CatalogServiceClient
	log  *slog.Logger
}

var _ usecase.CatalogReader = (*Client)(nil)

// New cria a conexão (preguiçosa: o gRPC só conecta na 1ª chamada). "extra"
// permite acrescentar opções (o dialer dos testes, o tracing da Task 8).
func New(cfg Config, extra ...grpc.DialOption) (*Client, error) {
	creds, err := cfg.TLS.credentials()
	if err != nil {
		return nil, err
	}
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		// O único retry do BFF é o da camada resilience; o do gRPC ficaria duplicado.
		grpc.WithDisableRetry(),
		grpc.WithChainUnaryInterceptor(requestIDInterceptor),
	}, extra...)
	conn, err := grpc.NewClient(cfg.Addr, opts...)
	if err != nil {
		return nil, err
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Client{conn: conn, rpc: catalogv1.NewCatalogServiceClient(conn), log: log}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// Conn expõe a conexão para a métrica de estado (bff_catalog_connection_state).
func (c *Client) Conn() *grpc.ClientConn { return c.conn }

// requestIDInterceptor manda o request_id do BFF ao catalog (x-request-id),
// para seguir a mesma requisição nos logs dos dois serviços.
func requestIDInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if id := reqmeta.RequestID(ctx); id != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, requestIDHeader, id)
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

func (c *Client) ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error) {
	req := &catalogv1.ListSpeciesRequest{Query: q.Q, State: q.State, PageSize: int32(q.Limit), PageToken: q.Cursor} //nolint:gosec // limit já validado (1..50)
	if q.Biome != "" {
		req.Biome = biomeToProto[q.Biome]
	}
	resp, err := c.rpc.ListSpecies(ctx, req)
	if err != nil {
		return domain.SpeciesPage{}, translateError(err)
	}
	m := mapper{ctx: ctx, log: c.log}
	page := domain.SpeciesPage{NextCursor: resp.GetNextPageToken(), Items: make([]domain.Summary, 0, len(resp.GetSpecies()))}
	for _, s := range resp.GetSpecies() {
		page.Items = append(page.Items, m.summary(s))
	}
	return page, nil
}

func (c *Client) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	resp, err := c.rpc.GetSpecies(ctx, &catalogv1.GetSpeciesRequest{Id: id})
	if err != nil {
		return domain.Species{}, translateError(err)
	}
	if resp.GetSpecies() == nil {
		return domain.Species{}, domain.NewError(domain.KindInternal, nil) // resposta inesperada
	}
	return mapper{ctx: ctx, log: c.log}.species(resp.GetSpecies()), nil
}

func (c *Client) ListFilters(ctx context.Context) (domain.Filters, error) {
	resp, err := c.rpc.ListFilters(ctx, &catalogv1.ListFiltersRequest{})
	if err != nil {
		return domain.Filters{}, translateError(err)
	}
	m := mapper{ctx: ctx, log: c.log}
	var f domain.Filters
	for _, b := range resp.GetBiomes() {
		if d, ok := m.biome(b.GetBiome()); ok {
			f.Biomes = append(f.Biomes, domain.BiomeCount{Biome: d, SpeciesCount: int(b.GetSpeciesCount())})
		}
	}
	for _, s := range resp.GetStates() {
		if uf, ok := m.uf(s.GetState()); ok {
			f.States = append(f.States, domain.StateCount{State: uf, SpeciesCount: int(s.GetSpeciesCount())})
		}
	}
	return f, nil
}
