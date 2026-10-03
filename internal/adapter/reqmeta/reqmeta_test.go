package reqmeta_test

import (
	"context"
	"sync"
	"testing"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
)

func TestMetaRoundTrip(t *testing.T) {
	m := reqmeta.New("abc")
	ctx := reqmeta.With(context.Background(), m)
	if reqmeta.RequestID(ctx) != "abc" {
		t.Fatal("request id perdido")
	}
	reqmeta.From(ctx).SetCache("stale")
	if m.Cache() != "stale" {
		t.Fatal("o registro é compartilhado: quem escreve no contexto escreve no mesmo Meta")
	}
}

func TestFromWithoutMetaIsSafe(t *testing.T) {
	m := reqmeta.From(context.Background())
	m.SetCache("hit") // não pode dar panic
	if reqmeta.RequestID(context.Background()) != "" {
		t.Fatal("sem Meta, o request id é vazio")
	}
}

func TestMetaIsConcurrencySafe(t *testing.T) {
	m := reqmeta.New("x")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.SetCache("miss"); _ = m.Cache() }()
	}
	wg.Wait() // rodando com -race, qualquer corrida falha o teste
}
