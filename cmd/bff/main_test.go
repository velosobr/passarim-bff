// Testes do ponto de entrada. Eles chamam run diretamente com um ambiente
// falso (um mapa no lugar das variáveis de ambiente) e conferem que o BFF
// falha cedo, com uma mensagem útil, quando a configuração está errada.
//
// Em Go, os testes ficam em arquivos terminados em _test.go e são funções
// TestXxx(t *testing.T); "go test" as encontra sozinho. t.Fatalf marca o teste
// como falho e o interrompe.

package main

import (
	"strings"
	"testing"
)

// Sem nenhuma variável definida, run tem de devolver um erro que cite
// CATALOG_ADDR, a primeira obrigatória. Quem sobe o container lê o log e sabe
// na hora o que faltou.
func TestRun_FailsFastNamingTheMissingVariable(t *testing.T) {
	err := run(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "CATALOG_ADDR") {
		t.Fatalf("sem configuração o processo não sobe e o erro cita a variável: %v", err)
	}
}

// A REDIS_URL pode carregar senha. Com uma URL inválida, o erro cita o NOME
// da variável, mas nunca o valor: senão a senha iria parar nos logs.
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
