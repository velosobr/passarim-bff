// Package domain guarda os tipos e as regras puras do BFF: nada de JSON,
// gRPC, Redis ou HTTP aqui. É a camada mais interna da arquitetura limpa.
//
// "Arquitetura limpa" quer dizer que as dependências apontam para dentro:
// adapter → usecase → domain. O domain não importa nenhum outro pacote do
// projeto e nenhuma biblioteca de infraestrutura, por isso as regras daqui
// podem ser testadas sem subir Redis, catalog ou servidor HTTP. Se um dia o
// BFF trocar de banco de cache ou de protocolo, este pacote não muda.
//
// Este arquivo cuida dos valores válidos de bioma e de UF.
package domain

import "sort"

// Biome é um bioma brasileiro. O valor é o MESMO código que o catalog usa
// no seu domínio e que a API REST expõe (/v1/filters e o parâmetro biome).
//
// "type Biome string" cria um tipo novo baseado em string. Ele se comporta
// como texto, mas o compilador não deixa passar uma string qualquer onde se
// espera um Biome sem conversão explícita: isso evita misturar bioma com UF.
type Biome string

// Os seis biomas, um por constante. Usar as constantes em vez de digitar
// "cerrado" no código evita erros de digitação que o compilador não pegaria.
const (
	BiomeAmazonia      Biome = "amazonia"
	BiomeMataAtlantica Biome = "mata_atlantica"
	BiomeCerrado       Biome = "cerrado"
	BiomeCaatinga      Biome = "caatinga"
	BiomePantanal      Biome = "pantanal"
	BiomePampa         Biome = "pampa"
)

// validBiomes funciona como um conjunto: consultar um map com uma chave que
// não existe devolve o valor zero do tipo (false), então validBiomes[b] já
// responde "é válido?" sem precisar de if.
var validBiomes = map[Biome]bool{
	BiomeAmazonia: true, BiomeMataAtlantica: true, BiomeCerrado: true,
	BiomeCaatinga: true, BiomePantanal: true, BiomePampa: true,
}

// ParseBiome aceita só os códigos exatos (sem diferenciar nada: "Cerrado" não vale).
//
// Devolve o bioma e um booleano que diz se o código é conhecido. Em Go,
// devolver (valor, ok) é o jeito comum de dizer "achei ou não achei" sem
// precisar de um erro.
func ParseBiome(s string) (Biome, bool) {
	b := Biome(s)
	return b, validBiomes[b]
}

// ConservationStatus é a categoria da lista vermelha da IUCN ("" = desconhecida).
//
// A IUCN (União Internacional para a Conservação da Natureza) classifica o
// risco de extinção de cada espécie: LC (pouco preocupante), NT, VU, EN, CR,
// EW, EX e DD (dados insuficientes). O domínio guarda só o código; o rótulo
// em português ("Vulnerável") é acrescentado na resposta pelo adapter HTTP.
type ConservationStatus string

// As 27 unidades federativas: 26 estados + Distrito Federal.
var validUFs = map[string]bool{
	"AC": true, "AL": true, "AP": true, "AM": true, "BA": true, "CE": true, "DF": true,
	"ES": true, "GO": true, "MA": true, "MT": true, "MS": true, "MG": true, "PA": true,
	"PB": true, "PR": true, "PE": true, "PI": true, "RJ": true, "RN": true, "RS": true,
	"RO": true, "RR": true, "SC": true, "SP": true, "SE": true, "TO": true,
}

// NormalizeUF converte para maiúsculas e valida.
//
// Assim "ms" e "MS" valem o mesmo, e a chave do cache fica igual para os
// dois (sem guardar a mesma resposta duas vezes).
func NormalizeUF(s string) (string, bool) {
	up := toUpperASCII(s)
	return up, validUFs[up]
}

// AllUFs devolve as siglas em ordem alfabética.
//
// A ordem de um map em Go é sorteada a cada execução, por isso copiamos as
// chaves para uma lista e a ordenamos. make(..., 0, len) já reserva o espaço
// da lista inteira, evitando realocações durante os append.
func AllUFs() []string {
	out := make([]string, 0, len(validUFs))
	for uf := range validUFs {
		out = append(out, uf)
	}
	sort.Strings(out)
	return out
}

// toUpperASCII converte só as letras a–z para A–Z, byte a byte.
//
// O strings.ToUpper da biblioteca padrão segue as regras do Unicode, em que
// algumas letras de fora do ASCII viram letras ASCII: o "ſ" (s longo) vira
// "S", então strings.ToUpper("ſp") daria "SP" e uma UF falsa passaria. Com a
// conversão simples, um texto estranho continua estranho e falha na validação.
func toUpperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}
