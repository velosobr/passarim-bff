// Chaves do Redis. Toda chave começa com o prefixo de versão, seguido do tipo
// de dado: "bff:v2:species:<id>", "bff:v2:filters:<audiência>",
// "bff:v2:list:<hash>" e "bff:v2:featured:<data>".

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// "v2" é a versão do FORMATO do que fica guardado: se o envelope ou um DTO
// gravado mudar, a versão sobe (as chaves antigas simplesmente deixam de ser
// lidas e expiram sozinhas). Subiu para v2 na frente B, quando o detalhe
// ganhou kids, primaryBiome e os picos do canto: com o v1, o BFF serviria um
// detalhe sem kids por até 10 minutos depois do deploy.
const keyPrefix = "bff:v2:"

// speciesKey é a chave de uma espécie. O id já foi validado (só a–z, 0–9 e
// hífen), então pode entrar direto na chave.
func speciesKey(id string) string { return keyPrefix + "species:" + id }

// filtersKey é a chave dos filtros de uma audiência: as contagens da
// jornada Kids só consideram as aves com conteúdo Kids.
func filtersKey(a domain.Audience) string { return keyPrefix + "filters:" + string(a) }

// featuredKey é a chave da "Ave do dia" de uma data. A data muda à
// meia-noite de São Paulo, e com ela a chave: a ave de ontem nunca sai, nem
// como dado velho.
func featuredKey(date string) string { return keyPrefix + "featured:" + date }

// listKey: o hash é calculado sobre o ListQuery JÁ normalizado (q aparado, UF em maiúsculas,
// limit efetivo), então consultas equivalentes compartilham a mesma chave. Os campos entram
// com tamanho na frente: cada um entra como "<tamanho>:<texto>", para "ab" +
// "c" nunca colidir com "a" + "bc".
//
// SHA-256 é uma função de hash: transforma qualquer texto num número de 256
// bits (64 caracteres em hexadecimal), sempre o mesmo para a mesma entrada.
// Usar o hash deixa a chave com tamanho fixo e sem caracteres estranhos,
// qualquer que seja o cursor.
func listKey(q domain.ListQuery) string {
	h := sha256.New()
	for _, part := range []string{q.Q, string(q.Biome), q.State, q.Cursor, strconv.Itoa(q.Limit), string(q.Audience), string(q.PrimaryBiome)} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}
	return keyPrefix + "list:" + hex.EncodeToString(h.Sum(nil))
}
