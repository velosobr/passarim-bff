package domain

// Os tipos abaixo guardam CHAVES de mídia (como o proto), nunca URLs:
// quem monta a URL pública é o adapter HTTP (assim trocar de CDN não muda o domínio).

type Credit struct {
	Author, License, Source, SourceURL string
}

type Photo struct {
	ThumbKey, MediumKey, LargeKey string
	Width, Height                 int
	Credit                        Credit
}

type Audio struct {
	Key        string
	DurationMs int
	Credit     Credit
}

type Fact struct {
	Text, Source string
}

type Cluster struct {
	Lat, Lng  float64
	Count     int
	Precision float64
}

// Summary é a versão leve usada nos cards da lista.
type Summary struct {
	ID, ScientificName, CommonName string
	ThumbnailKey                   string // vazio = ainda sem foto
	Conservation                   ConservationStatus
}

type SpeciesPage struct {
	Items      []Summary
	NextCursor string // vazio = não há mais páginas
}

// Species é a versão completa (tela de detalhe). Ponteiros = "pode não existir".
type Species struct {
	ID, ScientificName, CommonName, Family string
	SizeCm                                 *int
	Diet                                   *string // texto livre
	Conservation                           ConservationStatus
	Description                            string
	DescriptionCredit                      Credit
	Facts                                  []Fact
	Biomes                                 []Biome
	States                                 []string
	Photos                                 []Photo
	Audio                                  *Audio
	Clusters                               []Cluster
}

type BiomeCount struct {
	Biome        Biome
	SpeciesCount int
}

type StateCount struct {
	State        string
	SpeciesCount int
}

type Filters struct {
	Biomes []BiomeCount
	States []StateCount
}
