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

func speciesKey(id string) string { return keyPrefix + "species:" + id }

const filtersKey = keyPrefix + "filters"

// listKey: o hash é calculado sobre o ListQuery JÁ normalizado (q aparado, UF em maiúsculas,
// limit efetivo), então consultas equivalentes compartilham a mesma chave. Os campos entram
// com tamanho na frente para "a|b" + "c" nunca colidir com "a" + "b|c".
func listKey(q domain.ListQuery) string {
	h := sha256.New()
	for _, part := range []string{q.Q, string(q.Biome), q.State, q.Cursor, strconv.Itoa(q.Limit)} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}
	return keyPrefix + "list:" + hex.EncodeToString(h.Sum(nil))
}
