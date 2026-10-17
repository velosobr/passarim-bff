// Package telemetry liga o OpenTelemetry. Sem endpoint, o tracing fica DESLIGADO
// (o BFF sobe normalmente), mas o propagador W3C continua instalado para o
// traceparent recebido seguir até o catalog.
//
// Tracing distribuído: cada requisição ganha um "trace" com vários "spans"
// (pedaços com início e fim: a requisição HTTP, a chamada gRPC). O id do
// trace viaja entre os serviços no cabeçalho traceparent (padrão W3C), e
// assim uma ferramenta como o Jaeger mostra o caminho inteiro de uma
// requisição, do BFF ao catalog, com o tempo de cada parte.
package telemetry

import (
	"context"
	"errors"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Setup instala o propagador e, se houver endpoint, o exportador OTLP via
// gRPC (OTLP é o protocolo padrão do OpenTelemetry para enviar traces a um
// coletor, como o Jaeger). Devolve a função de desligamento, que envia os spans ainda no buffer
// (o "batcher" junta vários spans antes de enviar, para gastar menos rede).
// Com http:// a conexão com o coletor é sem TLS; com https://, com TLS.
func Setup(ctx context.Context, endpoint, serviceName string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, errors.New("OTEL_EXPORTER_OTLP_ENDPOINT: URL inválida (ex.: http://jaeger:4317)")
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(u.Host)}
	if u.Scheme != "https" {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exp, err := otlptracegrpc.New(ctx, opts...) // preguiçoso: não conecta aqui
	if err != nil {
		return nil, errors.New("OpenTelemetry: não foi possível criar o exportador")
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewSchemaless(semconv.ServiceName(serviceName))),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}
