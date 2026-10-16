// Rótulos em português de biomas e de status de conservação, mandados junto
// com os códigos nas respostas.

package httpadapter

import "github.com/velosobr/passarim-bff/internal/domain"

// Rótulos em português: o app mostra o texto pronto, sem manter a própria
// tradução dos códigos.
var biomeLabels = map[domain.Biome]string{
	domain.BiomeAmazonia: "Amazônia", domain.BiomeMataAtlantica: "Mata Atlântica", domain.BiomeCerrado: "Cerrado",
	domain.BiomeCaatinga: "Caatinga", domain.BiomePantanal: "Pantanal", domain.BiomePampa: "Pampa",
}

// conservationLabels traduz os códigos da IUCN.
var conservationLabels = map[domain.ConservationStatus]string{
	"LC": "Pouco preocupante", "NT": "Quase ameaçada", "VU": "Vulnerável", "EN": "Em perigo",
	"CR": "Criticamente em perigo", "EW": "Extinta na natureza", "EX": "Extinta", "DD": "Dados insuficientes",
}
