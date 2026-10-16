// Onde os bytes do cache ficam guardados: a interface Store e a
// implementação com Redis.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store é o armazenamento de bytes do cache (o Redis em produção; um mapa nos testes).
//
// Get devolve o valor, se a chave existe (found) e um erro só quando o Redis
// falhou. O cache trata "não existe" e "Redis fora do ar" de jeitos
// diferentes, por isso os dois casos não se misturam. Set grava com um TTL
// (tempo de vida): o Redis apaga a chave sozinho quando ele acaba.
type Store interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// RedisStore implementa Store com go-redis. Cada operação tem um timeout PRÓPRIO
// (REDIS_TIMEOUT): um Redis lento não pode comer o orçamento da requisição.
type RedisStore struct {
	c       *redis.Client
	timeout time.Duration
}

// NewRedisStore embrulha um cliente go-redis já criado.
func NewRedisStore(c *redis.Client, timeout time.Duration) *RedisStore {
	return &RedisStore{c: c, timeout: timeout}
}

// Get lê a chave dentro do prazo próprio. redis.Nil é o "erro" que o
// go-redis usa para chave inexistente; aqui ele vira found = false.
func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	v, err := s.c.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil // chave ausente não é erro
	}
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

// Set grava a chave com o TTL, também dentro do prazo próprio.
func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.c.Set(ctx, key, value, ttl).Err()
}

// NewRedisClient cria o cliente go-redis com TODOS os prazos presos ao REDIS_TIMEOUT.
// O go-redis tem DialTimeout de 5 s e o handshake da conexão não obedece ao prazo do
// contexto: sem isto, um Redis que aceita a conexão e não responde prenderia cada
// requisição por 5 s, estourando o orçamento de 3 s. A URL pode ter senha, então o
// erro nunca a repete.
func NewRedisClient(url string, timeout time.Duration) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, errors.New("REDIS_URL: URL inválida (ex.: redis://redis:6379)")
	}
	opt.MaxRetries = -1 // sem retries internos: o cache falha rápido e segue como miss
	opt.DialTimeout, opt.ReadTimeout, opt.WriteTimeout = timeout, timeout, timeout
	return redis.NewClient(opt), nil
}
