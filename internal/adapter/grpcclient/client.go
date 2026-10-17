// Package grpcclient implementa usecase.CatalogReader falando gRPC com o
// catalog. Traduz proto → domínio e código gRPC → domain.Kind; não decide
// retry, cache nem breaker (isso é das camadas de fora).
//
// gRPC é um protocolo de chamada remota: o BFF chama ListSpecies como se
// fosse uma função local, e o gRPC leva o pedido pela rede (HTTP/2) com os
// dados em Protocol Buffers, um formato binário compacto. O contrato está no
// catalog.proto do passarim-proto, e o Buf gera dele o código Go do cliente
// (o pacote catalogv1 importado abaixo). Por dentro do Passarim usamos gRPC;
// para o app, REST com JSON (ADR-0003).
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

// requestIDHeader é o nome do metadado gRPC (o equivalente a um cabeçalho
// HTTP) que leva o id da requisição ao catalog. Metadados gRPC usam nomes em
// minúsculas.
const requestIDHeader = "x-request-id"

// Config é o que New precisa: o endereço host:porta do catalog, as opções de
// TLS e o logger (opcional; sem ele, usa o slog padrão).
type Config struct {
	Addr   string
	TLS    TLS
	Logger *slog.Logger
}

// Client é o cliente gRPC do catalog. conn é a conexão (que o gRPC mantém e
// reabre sozinho), rpc é o cliente gerado a partir do proto e log avisa
// quando o catalog manda um dado que o BFF não conhece.
type Client struct {
	conn *grpc.ClientConn
	rpc  catalogv1.CatalogServiceClient
	log  *slog.Logger
}

// Esta linha não gera código: ela obriga o compilador a conferir que *Client
// implementa usecase.CatalogReader. Se um método faltar ou mudar de
// assinatura, o build quebra aqui, com uma mensagem clara.
var _ usecase.CatalogReader = (*Client)(nil)

// New cria a conexão (preguiçosa: o gRPC só conecta na 1ª chamada). "extra"
// permite acrescentar opções (o dialer dos testes, o tracing do main).
//
// "extra ...grpc.DialOption" é um parâmetro variádico: aceita zero ou mais
// opções, que chegam como uma lista. As opções fixas vêm primeiro e as
// extras são somadas no fim.
func New(cfg Config, extra ...grpc.DialOption) (*Client, error) {
	creds, err := cfg.TLS.credentials()
	if err != nil {
		return nil, err
	}
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		// Desliga o retry configurável do gRPC: quem repete é a camada resilience.
		// Ficam só os retries transparentes do gRPC (pedido que nem saiu pela rede).
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

// Close fecha a conexão. É chamado no desligamento do BFF.
func (c *Client) Close() error { return c.conn.Close() }

// Conn expõe a conexão para a métrica de estado (bff_catalog_connection_state).
func (c *Client) Conn() *grpc.ClientConn { return c.conn }

// requestIDInterceptor manda o request_id do BFF ao catalog (x-request-id),
// para seguir a mesma requisição nos logs dos dois serviços.
//
// Um interceptor é uma função que o gRPC chama em volta de cada chamada
// (como um middleware HTTP); "unário" é a chamada simples de um pedido e uma
// resposta. Ele acrescenta o metadado no contexto e chama invoker, que faz a
// chamada de verdade.
func requestIDInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if id := reqmeta.RequestID(ctx); id != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, requestIDHeader, id)
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

// ListSpecies converte a consulta do domínio no pedido do proto, chama o
// catalog e converte a resposta de volta. O bioma só é enviado quando há
// filtro: o valor zero do enum (UNSPECIFIED) já quer dizer "sem filtro".
//
// Os métodos Get* do código gerado (GetSpecies(), GetNextPageToken()) nunca
// quebram com resposta nil: devolvem o valor zero. Por isso o código os usa
// em vez de acessar os campos direto.
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

// GetSpecies busca uma espécie pelo id. Uma resposta sem espécie (o que o
// contrato não prevê) vira KindInternal, em vez de uma espécie vazia.
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

// ListFilters busca as contagens por bioma e por UF. Valores que o BFF não
// conhece (um bioma novo no proto, uma UF estranha) são descartados com um
// aviso no log, para o app nunca receber um filtro que não sabe usar.
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
