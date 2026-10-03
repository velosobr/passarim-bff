// Package reqmeta guarda, no contexto, os dados de UMA requisição que várias
// camadas precisam compartilhar: o request_id (para os logs) e o resultado do
// cache (para o cabeçalho X-Cache e o log de acesso). É um registro MUTÁVEL:
// o handler cria, o decorator de cache escreve, o handler lê depois.
package reqmeta

import (
	"context"
	"sync"
)

type Meta struct {
	mu        sync.Mutex
	requestID string
	cache     string
}

func New(requestID string) *Meta { return &Meta{requestID: requestID} }

func (m *Meta) RequestID() string { return m.requestID }

func (m *Meta) SetCache(result string) {
	m.mu.Lock()
	m.cache = result
	m.mu.Unlock()
}

func (m *Meta) Cache() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cache
}

type ctxKey struct{}

func With(ctx context.Context, m *Meta) context.Context { return context.WithValue(ctx, ctxKey{}, m) }

// From nunca devolve nil: sem registro no contexto, devolve um descartável
// (assim os decorators não precisam checar nil em cada uso).
func From(ctx context.Context) *Meta {
	if m, ok := ctx.Value(ctxKey{}).(*Meta); ok {
		return m
	}
	return &Meta{}
}

func RequestID(ctx context.Context) string { return From(ctx).requestID }
