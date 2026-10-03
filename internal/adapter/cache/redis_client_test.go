package cache_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/cache"
)

// Um servidor que aceita a conexão e fica calado (como o proxy de portas do Docker diante de um
// container parado) não pode prender a requisição além do REDIS_TIMEOUT: o handshake do go-redis
// usa DialTimeout (5 s por padrão), então o cliente precisa ser criado com o timeout configurado.
func TestNewRedisClient_SilentServerIsBoundedByTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c // aceita e não responde
		}
	}()
	client, err := cache.NewRedisClient("redis://"+l.Addr().String(), 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	st := cache.NewRedisStore(client, 200*time.Millisecond)
	start := time.Now()
	if _, _, err := st.Get(context.Background(), "k"); err == nil {
		t.Fatal("servidor mudo: Get deveria dar erro")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("o timeout configurado (200ms) deveria limitar a espera, levou %v", d)
	}
}

func TestNewRedisClient_InvalidURLNeverEchoesTheValue(t *testing.T) {
	_, err := cache.NewRedisClient("isto-nao-e-url://:senha-secreta@host", time.Second)
	if err == nil || strings.Contains(err.Error(), "senha-secreta") || !strings.Contains(err.Error(), "REDIS_URL") {
		t.Fatalf("erro deve citar REDIS_URL e nunca o valor: %v", err)
	}
}
