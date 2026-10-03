package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/velosobr/passarim-bff/internal/adapter/cache"
)

func startRedis(t *testing.T) (*redis.Client, *tcredis.RedisContainer) {
	t.Helper()
	if testing.Short() {
		t.Skip("integração: precisa de Docker")
	}
	ctx := context.Background()
	ctr, err := tcredis.Run(ctx, "redis:8-alpine")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })
	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cache.NewRedisClient(uri, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, ctr
}

func TestRedisStore_GetSetAndTTL(t *testing.T) {
	client, _ := startRedis(t)
	st := cache.NewRedisStore(client, 500*time.Millisecond)
	ctx := context.Background()
	if _, found, err := st.Get(ctx, "bff:v1:x"); err != nil || found {
		t.Fatalf("chave ausente: found=%v err=%v", found, err)
	}
	if err := st.Set(ctx, "bff:v1:x", []byte(`{"a":1}`), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	v, found, err := st.Get(ctx, "bff:v1:x")
	if err != nil || !found || string(v) != `{"a":1}` {
		t.Fatalf("get: %q %v %v", v, found, err)
	}
	if ttl := client.TTL(ctx, "bff:v1:x").Val(); ttl < time.Hour || ttl > 2*time.Hour {
		t.Fatalf("TTL aplicado deveria ser ~2h: %v", ttl)
	}
}

func TestRedisStore_RedisDownReturnsErrorQuickly(t *testing.T) {
	client, ctr := startRedis(t)
	st := cache.NewRedisStore(client, 200*time.Millisecond)
	if err := ctr.Stop(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, _, err := st.Get(context.Background(), "k"); err == nil {
		t.Fatal("com o Redis parado, Get deveria dar erro")
	}
	if err := st.Set(context.Background(), "k", []byte("v"), time.Minute); err == nil {
		t.Fatal("com o Redis parado, Set deveria dar erro")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("o timeout por operação deveria limitar a espera: %v", time.Since(start))
	}
}
