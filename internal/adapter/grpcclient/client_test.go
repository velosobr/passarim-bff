// Testes do cliente gRPC contra um catalog falso que roda na memória. Eles
// conferem a tradução proto → domínio, o descarte de valores desconhecidos,
// a tradução dos códigos de erro, o envio do request_id, a ausência de retry
// próprio do gRPC e os erros de configuração de TLS.

package grpcclient_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	catalogv1 "github.com/velosobr/passarim-proto/gen/go/passarim/catalog/v1"

	"github.com/velosobr/passarim-bff/internal/adapter/grpcclient"
	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// fakeServer é um catalog de mentira: cada teste configura o que ele responde.
//
// Embutir catalogv1.UnimplementedCatalogServiceServer (o campo sem nome) dá à
// struct todos os métodos do serviço, que respondem "não implementado"; o
// fake só sobrescreve os três que usa. calls é atômico porque o servidor gRPC
// atende em outra goroutine, e lastMD guarda os metadados recebidos.
type fakeServer struct {
	catalogv1.UnimplementedCatalogServiceServer
	list    func(*catalogv1.ListSpeciesRequest) (*catalogv1.ListSpeciesResponse, error)
	get     func(*catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error)
	filters func() (*catalogv1.ListFiltersResponse, error)
	calls   atomic.Int32
	lastMD  metadata.MD
}

// Cada método conta a chamada, guarda os metadados (quando o teste precisa)
// e devolve o que a função configurada no teste mandar.
func (f *fakeServer) ListSpecies(ctx context.Context, r *catalogv1.ListSpeciesRequest) (*catalogv1.ListSpeciesResponse, error) {
	f.calls.Add(1)
	f.lastMD, _ = metadata.FromIncomingContext(ctx)
	return f.list(r)
}

func (f *fakeServer) GetSpecies(ctx context.Context, r *catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error) {
	f.calls.Add(1)
	f.lastMD, _ = metadata.FromIncomingContext(ctx)
	return f.get(r)
}

func (f *fakeServer) ListFilters(ctx context.Context, _ *catalogv1.ListFiltersRequest) (*catalogv1.ListFiltersResponse, error) {
	f.calls.Add(1)
	return f.filters()
}

// start sobe o servidor em memória (bufconn: sem rede de verdade) e devolve o cliente do BFF.
//
// bufconn é uma "conexão de rede" que vive num buffer na memória (aqui, de
// 1 MiB): o cliente e o servidor conversam por gRPC de verdade, sem abrir
// porta nenhuma. O endereço "passthrough:///bufnet" é só um nome, e o
// WithContextDialer troca a discagem real pela do buffer. t.Cleanup agenda
// o desligamento para o fim de cada teste.
func start(t *testing.T, srv *fakeServer) *grpcclient.Client {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	catalogv1.RegisterCatalogServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	c, err := grpcclient.New(grpcclient.Config{Addr: "passthrough:///bufnet", Logger: slog.New(slog.DiscardHandler)},
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// Uma espécie completa do proto vira a espécie do domínio campo a campo. O
// fake inclui um bioma UNSPECIFIED, um bioma de número 99 (que não existe) e
// a UF "xx": os três somem da resposta, e o resto chega intacto.
func TestGetSpecies_MapsProtoToDomain(t *testing.T) {
	size, diet := int32(23), "Frutos e insetos"
	srv := &fakeServer{get: func(r *catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error) {
		return &catalogv1.GetSpeciesResponse{Species: &catalogv1.Species{
			Id: r.GetId(), ScientificName: "Turdus rufiventris", CommonNamePt: "Sabiá-laranjeira", Family: "Turdidae",
			SizeCm: &size, Diet: &diet, ConservationStatus: catalogv1.ConservationStatus_CONSERVATION_STATUS_LC,
			Description: "d", DescriptionCredit: &catalogv1.Credit{Author: "a", License: "CC-BY", Source: "curated", SourceUrl: "https://x"},
			Facts:    []*catalogv1.Fact{{Text: "t", Source: "s"}},
			Biomes:   []catalogv1.Biome{catalogv1.Biome_BIOME_MATA_ATLANTICA, catalogv1.Biome_BIOME_UNSPECIFIED, catalogv1.Biome(99)},
			States:   []string{"SP", "xx", "RJ"},
			Photos:   []*catalogv1.Photo{{ThumbKey: "t", MediumKey: "m", LargeKey: "l", Width: 1600, Height: 900, Credit: &catalogv1.Credit{Author: "p"}}},
			Audio:    &catalogv1.Audio{Key: "audio-1.aac", DurationMs: 21500, Credit: &catalogv1.Credit{Author: "z"}},
			Clusters: []*catalogv1.OccurrenceCluster{{Lat: -23.5, Lng: -46.6, Count: 4, Precision: 0.5}},
		}}, nil
	}}
	c := start(t, srv)
	s, err := c.GetSpecies(context.Background(), "turdus-rufiventris")
	if err != nil {
		t.Fatal(err)
	}
	if s.CommonName != "Sabiá-laranjeira" || s.Conservation != "LC" || *s.SizeCm != 23 || *s.Diet != "Frutos e insetos" {
		t.Fatalf("campos básicos: %+v", s)
	}
	if len(s.Biomes) != 1 || s.Biomes[0] != domain.BiomeMataAtlantica {
		t.Fatalf("biomas desconhecidos/UNSPECIFIED deveriam ser descartados: %v", s.Biomes)
	}
	if len(s.States) != 2 || s.States[0] != "SP" || s.States[1] != "RJ" {
		t.Fatalf("UF desconhecida deveria ser descartada: %v", s.States)
	}
	if len(s.Photos) != 1 || s.Photos[0].LargeKey != "l" || s.Audio == nil || s.Audio.Key != "audio-1.aac" || len(s.Clusters) != 1 || s.Clusters[0].Count != 4 {
		t.Fatalf("mídia e clusters: %+v", s)
	}
}

// Ausências: conservação UNSPECIFIED vira "", áudio com chave vazia vira
// "sem áudio" (nil), e tamanho e dieta ausentes ficam nil.
func TestGetSpecies_UnspecifiedConservationIsEmpty_AudioMissingIsNil(t *testing.T) {
	srv := &fakeServer{get: func(*catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error) {
		return &catalogv1.GetSpeciesResponse{Species: &catalogv1.Species{Id: "x", Audio: &catalogv1.Audio{Key: ""}}}, nil
	}}
	s, err := start(t, srv).GetSpecies(context.Background(), "x")
	if err != nil || s.Conservation != "" || s.Audio != nil || s.SizeCm != nil || s.Diet != nil {
		t.Fatalf("got %+v %v", s, err)
	}
}

// Uma resposta OK sem espécie dentro é tratada como bug do catalog.
func TestGetSpecies_EmptyResponseIsInternal(t *testing.T) {
	srv := &fakeServer{get: func(*catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error) {
		return &catalogv1.GetSpeciesResponse{}, nil // sem species: resposta inesperada
	}}
	_, err := start(t, srv).GetSpecies(context.Background(), "x")
	if domain.KindOf(err) != domain.KindInternal {
		t.Fatalf("esperava Internal, veio %v", err)
	}
}

// Cada filtro da consulta chega ao pedido gRPC no campo certo, e a resposta
// volta como uma página com os resumos e o cursor da próxima.
func TestListSpecies_TranslatesRequestAndResponse(t *testing.T) {
	var got *catalogv1.ListSpeciesRequest
	srv := &fakeServer{list: func(r *catalogv1.ListSpeciesRequest) (*catalogv1.ListSpeciesResponse, error) {
		got = r
		return &catalogv1.ListSpeciesResponse{
			Species: []*catalogv1.SpeciesSummary{
				{Id: "a", ScientificName: "A a", CommonNamePt: "A", ThumbnailKey: "k", ConservationStatus: catalogv1.ConservationStatus_CONSERVATION_STATUS_VU},
				{Id: "b"},
			},
			NextPageToken: "tok",
		}, nil
	}}
	page, err := start(t, srv).ListSpecies(context.Background(), domain.ListQuery{Q: "sabia", Biome: domain.BiomeCerrado, State: "SP", Cursor: "c", Limit: 7})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetQuery() != "sabia" || got.GetBiome() != catalogv1.Biome_BIOME_CERRADO || got.GetState() != "SP" || got.GetPageSize() != 7 || got.GetPageToken() != "c" {
		t.Fatalf("pedido ao catalog: %v", got)
	}
	if len(page.Items) != 2 || page.NextCursor != "tok" || page.Items[0].Conservation != "VU" || page.Items[0].ThumbnailKey != "k" || page.Items[1].Conservation != "" {
		t.Fatalf("resposta: %+v", page)
	}
}

// Nos filtros, o bioma UNSPECIFIED e a UF "ZZ" são descartados.
func TestListFilters_DropsUnknownBiomeAndState(t *testing.T) {
	srv := &fakeServer{filters: func() (*catalogv1.ListFiltersResponse, error) {
		return &catalogv1.ListFiltersResponse{
			Biomes: []*catalogv1.BiomeCount{{Biome: catalogv1.Biome_BIOME_PAMPA, SpeciesCount: 3}, {Biome: catalogv1.Biome_BIOME_UNSPECIFIED, SpeciesCount: 9}},
			States: []*catalogv1.StateCount{{State: "RS", SpeciesCount: 5}, {State: "ZZ", SpeciesCount: 1}},
		}, nil
	}}
	f, err := start(t, srv).ListFilters(context.Background())
	if err != nil || len(f.Biomes) != 1 || f.Biomes[0].Biome != domain.BiomePampa || f.Biomes[0].SpeciesCount != 3 || len(f.States) != 1 || f.States[0].State != "RS" {
		t.Fatalf("got %+v %v", f, err)
	}
}

// Cada código gRPC cai num Kind da tabela única (spec §5.3). O fake devolve
// uma mensagem "interna", e o teste só olha o Kind: a mensagem do catalog
// nunca deve decidir nada no BFF.
func TestErrorCodesMapToKinds(t *testing.T) {
	cases := map[codes.Code]domain.Kind{
		codes.Unavailable: domain.KindUnavailable, codes.DeadlineExceeded: domain.KindTimeout,
		codes.Internal: domain.KindUpstream, codes.Unknown: domain.KindUpstream, codes.ResourceExhausted: domain.KindUpstream,
		codes.NotFound: domain.KindNotFound, codes.InvalidArgument: domain.KindInvalidArgument, codes.Canceled: domain.KindCanceled,
		codes.Unimplemented: domain.KindInternal, codes.PermissionDenied: domain.KindInternal, codes.FailedPrecondition: domain.KindInternal,
	}
	for code, want := range cases {
		srv := &fakeServer{get: func(*catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error) {
			return nil, status.Error(code, "mensagem-interna-do-catalog")
		}}
		_, err := start(t, srv).GetSpecies(context.Background(), "x")
		if got := domain.KindOf(err); got != want {
			t.Errorf("%s → %s, esperado %s", code, got, want)
		}
	}
}

// O request_id do contexto chega ao catalog no metadado x-request-id, para
// a mesma requisição poder ser seguida nos logs dos dois serviços.
func TestSendsRequestIDMetadata(t *testing.T) {
	srv := &fakeServer{get: func(*catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error) {
		return &catalogv1.GetSpeciesResponse{Species: &catalogv1.Species{Id: "x"}}, nil
	}}
	c := start(t, srv)
	ctx := reqmeta.With(context.Background(), reqmeta.New("req-123"))
	if _, err := c.GetSpecies(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	if v := srv.lastMD.Get("x-request-id"); len(v) != 1 || v[0] != "req-123" {
		t.Fatalf("metadado x-request-id: %v", v)
	}
}

// grpc.WithDisableRetry: o único retry do BFF é o nosso (camada resilience).
// Com Unavailable, o cliente faz uma chamada só; quem repete é o decorator
// de retry, que fica do lado de fora.
func TestDoesNotRetryByItself(t *testing.T) {
	srv := &fakeServer{get: func(*catalogv1.GetSpeciesRequest) (*catalogv1.GetSpeciesResponse, error) {
		return nil, status.Error(codes.Unavailable, "x")
	}}
	_, _ = start(t, srv).GetSpecies(context.Background(), "x")
	if srv.calls.Load() != 1 {
		t.Fatalf("esperava 1 chamada, houve %d", srv.calls.Load())
	}
}

// Configurações de TLS erradas falham já no New (CA inexistente, CA que não
// é certificado, par de certificado inválido), e sem TLS o cliente é criado
// normalmente. 0o600 é a permissão do arquivo em octal: leitura e escrita só
// para o dono.
func TestTLSConfigErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(bad, []byte("isto não é um certificado"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tls := range map[string]grpcclient.TLS{
		"CA inexistente": {CAFile: filepath.Join(dir, "nao-existe.pem")},
		"CA inválida":    {CAFile: bad},
		"cert sem chave": {CAFile: bad, CertFile: bad, KeyFile: bad},
	} {
		if _, err := grpcclient.New(grpcclient.Config{Addr: "localhost:1", TLS: tls}); err == nil {
			t.Errorf("%s: deveria falhar", name)
		}
	}
	// Sem TLS configurado, sobe "insecure" (rede privada local) e não conecta de verdade até a 1ª chamada.
	c, err := grpcclient.New(grpcclient.Config{Addr: "localhost:1"})
	if err != nil {
		t.Fatalf("sem TLS deveria criar o cliente: %v", err)
	}
	_ = c.Close()
	if errors.Is(err, context.Canceled) {
		t.Fatal("inesperado")
	}
}
