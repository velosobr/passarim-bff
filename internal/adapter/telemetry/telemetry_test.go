package telemetry_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/velosobr/passarim-bff/internal/adapter/telemetry"
)

func TestSetup_DisabledWithoutEndpointStillInstallsPropagator(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(), "", "passarim-bff")
	if err != nil {
		t.Fatalf("sem endpoint o tracing fica desligado, sem erro: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown do modo desligado: %v", err)
	}
	found := false
	for _, f := range otel.GetTextMapPropagator().Fields() {
		if f == "traceparent" {
			found = true
		}
	}
	if !found {
		t.Fatal("o propagador W3C traceparent deveria estar instalado (a propagação ao catalog independe do exporter)")
	}
}

func TestSetup_WithEndpointCreatesProviderWithoutConnecting(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(), "http://127.0.0.1:1", "passarim-bff")
	if err != nil {
		t.Fatalf("o exporter OTLP é preguiçoso: não conecta no Setup. err=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = shutdown(ctx) // pode devolver erro de flush (ninguém escuta); o que importa é não travar
}

func TestSetup_RejectsInvalidEndpoint(t *testing.T) {
	if _, err := telemetry.Setup(context.Background(), "://lixo", "x"); err == nil {
		t.Fatal("endpoint inválido deveria falhar")
	}
}
