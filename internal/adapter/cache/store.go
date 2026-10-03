package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store é o armazenamento de bytes do cache (o Redis em produção; um mapa nos testes).
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

func NewRedisStore(c *redis.Client, timeout time.Duration) *RedisStore {
	return &RedisStore{c: c, timeout: timeout}
}

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
