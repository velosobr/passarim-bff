// Package reqmeta guarda, no contexto, os dados de UMA requisição que várias
// camadas precisam compartilhar: o request_id (para os logs) e o resultado do
// cache (para o cabeçalho X-Cache e o log de acesso). É um registro MUTÁVEL:
// o handler cria, o decorator de cache escreve, o handler lê depois.
//
// Um context.Context pode carregar valores além do prazo: context.WithValue
// cria um contexto filho com um par chave/valor. Como o contexto passa por
// todas as camadas da requisição, ele é o lugar natural para um dado que
// pertence a ela. Guardamos um ponteiro, e não um valor, justamente para as
// camadas de dentro escreverem e as de fora lerem o mesmo registro.
package reqmeta

import (
	"context"
	"sync"
)

// Meta é o registro de uma requisição. O sync.Mutex (uma trava) protege o
// campo cache: só uma goroutine por vez o lê ou escreve. O requestID não
// precisa da trava porque é definido na criação e nunca muda.
type Meta struct {
	mu        sync.Mutex
	requestID string
	cache     string
}

// New cria o registro com o id da requisição.
func New(requestID string) *Meta { return &Meta{requestID: requestID} }

// RequestID devolve o id da requisição.
func (m *Meta) RequestID() string { return m.requestID }

// SetCache grava o resultado do cache (por exemplo, "hit", "miss" ou
// "stale"), com a trava fechada durante a escrita.
func (m *Meta) SetCache(result string) {
	m.mu.Lock()
	m.cache = result
	m.mu.Unlock()
}

// Cache lê o resultado do cache. O defer destrava mesmo se algo der errado
// depois do Lock.
func (m *Meta) Cache() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cache
}

// ctxKey é a chave do valor no contexto. Ser um tipo próprio e não exportado
// garante que nenhum outro pacote use a mesma chave sem querer, o que
// aconteceria com uma string como "meta".
type ctxKey struct{}

// With devolve um contexto filho que carrega o registro.
func With(ctx context.Context, m *Meta) context.Context { return context.WithValue(ctx, ctxKey{}, m) }

// From nunca devolve nil: sem registro no contexto, devolve um descartável
// (assim os decorators não precisam checar nil em cada uso).
func From(ctx context.Context) *Meta {
	if m, ok := ctx.Value(ctxKey{}).(*Meta); ok {
		return m
	}
	return &Meta{}
}

// RequestID é um atalho para From(ctx).RequestID(): "" quando não há registro.
func RequestID(ctx context.Context) string { return From(ctx).requestID }
