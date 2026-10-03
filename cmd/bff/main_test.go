package main

import (
	"strings"
	"testing"
)

func TestRun_FailsFastNamingTheMissingVariable(t *testing.T) {
	err := run(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "CATALOG_ADDR") {
		t.Fatalf("sem configuração o processo não sobe e o erro cita a variável: %v", err)
	}
}

func TestRun_InvalidRedisURLNeverEchoesTheValue(t *testing.T) {
	env := map[string]string{
		"CATALOG_ADDR": "catalog:50051", "MEDIA_BASE_URL": "http://localhost:8888/b",
		"REDIS_URL": "isto-nao-e-url://:senha-secreta@host", "HTTP_ADDR": "127.0.0.1:0", "METRICS_ADDR": "127.0.0.1:0",
	}
	err := run(func(k string) string { return env[k] })
	if err == nil || strings.Contains(err.Error(), "senha-secreta") || !strings.Contains(err.Error(), "REDIS_URL") {
		t.Fatalf("URL do Redis inválida: o erro cita o nome da variável e nunca o valor: %v", err)
	}
}
