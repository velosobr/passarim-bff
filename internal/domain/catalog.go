// Package domain guarda os tipos e as regras puras do BFF: nada de JSON,
// gRPC, Redis ou HTTP aqui. É a camada mais interna da arquitetura limpa.
package domain

import "sort"

// Biome é um bioma brasileiro. O valor é o MESMO código que o catalog usa
// no seu domínio e que a API REST expõe (/v1/filters e o parâmetro biome).
type Biome string

const (
	BiomeAmazonia      Biome = "amazonia"
	BiomeMataAtlantica Biome = "mata_atlantica"
	BiomeCerrado       Biome = "cerrado"
	BiomeCaatinga      Biome = "caatinga"
	BiomePantanal      Biome = "pantanal"
	BiomePampa         Biome = "pampa"
)

var validBiomes = map[Biome]bool{
	BiomeAmazonia: true, BiomeMataAtlantica: true, BiomeCerrado: true,
	BiomeCaatinga: true, BiomePantanal: true, BiomePampa: true,
}

// ParseBiome aceita só os códigos exatos (sem diferenciar nada: "Cerrado" não vale).
func ParseBiome(s string) (Biome, bool) {
	b := Biome(s)
	return b, validBiomes[b]
}

// ConservationStatus é a categoria da lista vermelha da IUCN ("" = desconhecida).
type ConservationStatus string

// As 27 unidades federativas: 26 estados + Distrito Federal.
var validUFs = map[string]bool{
	"AC": true, "AL": true, "AP": true, "AM": true, "BA": true, "CE": true, "DF": true,
	"ES": true, "GO": true, "MA": true, "MT": true, "MS": true, "MG": true, "PA": true,
	"PB": true, "PR": true, "PE": true, "PI": true, "RJ": true, "RN": true, "RS": true,
	"RO": true, "RR": true, "SC": true, "SP": true, "SE": true, "TO": true,
}

// NormalizeUF converte para maiúsculas e valida.
func NormalizeUF(s string) (string, bool) {
	up := toUpperASCII(s)
	return up, validUFs[up]
}

// AllUFs devolve as siglas em ordem alfabética.
func AllUFs() []string {
	out := make([]string, 0, len(validUFs))
	for uf := range validUFs {
		out = append(out, uf)
	}
	sort.Strings(out)
	return out
}

func toUpperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}
