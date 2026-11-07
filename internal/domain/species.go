// Os dados de uma espécie como o BFF os entende, já sem nada do formato gRPC.
// O grpcclient converte as mensagens do proto nestes tipos, e o adapter HTTP
// converte estes tipos nos DTOs JSON que o app recebe.

package domain

// Os tipos abaixo guardam CHAVES de mídia (como o proto), nunca URLs:
// quem monta a URL pública é o adapter HTTP (assim trocar de CDN não muda o domínio).

// Credit é o crédito de autoria de uma foto, de um canto ou de um texto. As
// licenças Creative Commons exigem mostrar o autor e a licença; Source diz de
// onde veio (iNaturalist, xeno-canto, Wikipedia ou curadoria) e SourceURL
// aponta para o original.
type Credit struct {
	Author, License, Source, SourceURL string
}

// Photo é uma foto em três tamanhos (miniatura, média e grande), cada um com
// a sua chave no object storage. Width e Height são do tamanho grande.
type Photo struct {
	ThumbKey, MediumKey, LargeKey string
	Width, Height                 int
	Credit                        Credit
}

// Audio é a gravação do canto, com a duração em milissegundos. Peaks são
// os 64 picos da forma de onda, de 0 a 1, que o player desenha (frente B);
// vazio num canto gravado antes de o catalog calcular os picos.
type Audio struct {
	Key        string
	DurationMs int
	Credit     Credit
	Peaks      []float32
}

// KidsContent é o texto para crianças da espécie (frente B), gerado a partir
// do texto adulto e revisado por uma pessoa no catalog. Os fatos chegam só
// com o texto: de qual fato adulto cada um saiu não interessa ao app.
type KidsContent struct {
	Description string
	Facts       []string
	DidYouKnow  string
}

// Fact é uma curiosidade sobre a espécie, com a fonte de onde ela saiu.
type Fact struct {
	Text, Source string
}

// Cluster é um grupo de avistamentos no mapa: um ponto (Lat, Lng) que
// representa Count observações próximas. Precision é o tamanho do quadrado
// usado para agrupar, em graus; o app o usa para escolher o raio do círculo.
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
	PrimaryBiome                   Biome // o "mundo" da ave no Kids; vazio = sem bioma principal
}

// SpeciesPage é uma página da lista. A paginação é por cursor: para buscar a
// próxima página, o cliente devolve o NextCursor recebido.
type SpeciesPage struct {
	Items      []Summary
	NextCursor string // vazio = não há mais páginas
}

// Species é a versão completa (tela de detalhe). Ponteiros = "pode não existir".
//
// Um *int pode ser nil, o que diferencia "não sabemos o tamanho" de "tamanho
// 0". Já as listas vazias (nil) bastam para dizer "nenhum": não precisam de
// ponteiro. Audio é ponteiro porque há espécies sem canto gravado. A primeira
// foto de Photos é a principal.
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
	PrimaryBiome                           Biome        // o "mundo" da ave no Kids; vazio = sem bioma principal
	Kids                                   *KidsContent // nil = a espécie ainda não tem conteúdo Kids
}

// BiomeCount diz quantas espécies existem num bioma (usado nos filtros).
type BiomeCount struct {
	Biome        Biome
	SpeciesCount int
}

// StateCount diz quantas espécies existem numa UF (usado nos filtros).
type StateCount struct {
	State        string
	SpeciesCount int
}

// Filters são as opções da tela de filtros: só os biomas e as UFs que têm
// pelo menos uma espécie, cada um com a contagem.
//
// PrimaryBiomes conta por bioma principal, o "mundo" de cada ave no Kids: é
// a conta do álbum ("4 de 9" no Pantanal).
type Filters struct {
	Biomes        []BiomeCount
	States        []StateCount
	PrimaryBiomes []BiomeCount
}

// Featured é a "Ave do dia" que o catalog escolheu: o resumo (o card) e o
// canto, nil quando a ave não tem canto.
type Featured struct {
	Species Summary
	Audio   *Audio
}
