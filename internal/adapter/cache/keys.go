// Chaves do Redis. Toda chave começa com o prefixo de versão, seguido do tipo
// de dado: "bff:v1:species:<id>", "bff:v1:filters" e "bff:v1:list:<hash>".
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// "v1" é a versão do FORMATO do envelope: se a struct do envelope mudar, suba para v2
// (as chaves antigas simplesmente deixam de ser lidas e expiram sozinhas).
const keyPrefix = "bff:v1:"

// speciesKey é a chave de uma espécie. O id já foi validado (só a–z, 0–9 e
// hífen), então pode entrar direto na chave.
func speciesKey(id string) string { return keyPrefix + "species:" + id }

// filtersKey é a chave única dos filtros: a resposta não depende de nenhum
// parâmetro.
const filtersKey = keyPrefix + "filters"

// listKey: o hash é calculado sobre o ListQuery JÁ normalizado (q aparado, UF em maiúsculas,
// limit efetivo), então consultas equivalentes compartilham a mesma chave. Os campos entram
// com tamanho na frente para "a|b" + "c" nunca colidir com "a" + "b|c".
//
// SHA-256 é uma função de hash: transforma qualquer texto num número de 256
// bits (64 caracteres em hexadecimal), sempre o mesmo para a mesma entrada.
// Usar o hash deixa a chave com tamanho fixo e sem caracteres estranhos,
// qualquer que seja o cursor.
func listKey(q domain.ListQuery) string {
	h := sha256.New()
	for _, part := range []string{q.Q, string(q.Biome), q.State, q.Cursor, strconv.Itoa(q.Limit)} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}
	return keyPrefix + "list:" + hex.EncodeToString(h.Sum(nil))
}
