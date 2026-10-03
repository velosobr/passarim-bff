package httpadapter

import "github.com/velosobr/passarim-bff/internal/domain"

// Rótulos em português: o app não precisa conhecer os códigos.
var biomeLabels = map[domain.Biome]string{
	domain.BiomeAmazonia: "Amazônia", domain.BiomeMataAtlantica: "Mata Atlântica", domain.BiomeCerrado: "Cerrado",
	domain.BiomeCaatinga: "Caatinga", domain.BiomePantanal: "Pantanal", domain.BiomePampa: "Pampa",
}

var conservationLabels = map[domain.ConservationStatus]string{
	"LC": "Pouco preocupante", "NT": "Quase ameaçada", "VU": "Vulnerável", "EN": "Em perigo",
	"CR": "Criticamente em perigo", "EW": "Extinta na natureza", "EX": "Extinta", "DD": "Dados insuficientes",
}
