// DTOs da API REST e as conversões do domínio para eles.

package httpadapter

import (
	"context"

	"github.com/velosobr/passarim-bff/internal/adapter/media"
	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

// DTOs = o contrato REST. São structs PRÓPRIAS: nunca serializamos o domínio
// nem o proto (OWASP API3: só sai o que está listado aqui).
//
// OWASP API3 é o risco de "exposição excessiva de propriedades": serializar
// uma struct interna inteira poderia vazar campos que o app não deveria ver.
// Com DTOs próprios, um campo novo no domínio só sai na API se alguém o
// acrescentar aqui, de propósito. Os nomes JSON (camelCase) são o contrato
// descrito no openapi.yaml. Ponteiros viram null no JSON quando nil.

// labelDTO é um código com o rótulo legível ("VU", "Vulnerável").
type labelDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// creditDTO é o crédito de autoria.
type creditDTO struct {
	Author    string `json:"author"`
	License   string `json:"license"`
	Source    string `json:"source"`
	SourceURL string `json:"sourceUrl"`
}

// summaryDTO é o card da lista. thumbnailUrl é null quando ainda não há foto,
// e primaryBiome (o "mundo" da ave no Kids) é null quando a ave não tem.
type summaryDTO struct {
	ID                 string    `json:"id"`
	CommonName         string    `json:"commonName"`
	ScientificName     string    `json:"scientificName"`
	ThumbnailURL       *string   `json:"thumbnailUrl"`
	ConservationStatus *labelDTO `json:"conservationStatus"`
	PrimaryBiome       *string   `json:"primaryBiome"`
}

// speciesPageDTO é uma página; nextCursor é null na última.
type speciesPageDTO struct {
	Items      []summaryDTO `json:"items"`
	NextCursor *string      `json:"nextCursor"`
}

// factDTO é uma curiosidade.
type factDTO struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// photoDTO é uma foto com as URLs dos três tamanhos.
type photoDTO struct {
	ThumbURL  *string   `json:"thumbUrl"`
	MediumURL *string   `json:"mediumUrl"`
	LargeURL  *string   `json:"largeUrl"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Credit    creditDTO `json:"credit"`
}

// audioDTO é o canto, já com a URL pública. peaks são os 64 picos da forma
// de onda, de 0 a 1; sai [] (nunca null) quando o catalog ainda não os
// calculou, e o app desenha uma forma genérica.
type audioDTO struct {
	URL        string    `json:"url"`
	DurationMs int       `json:"durationMs"`
	Credit     creditDTO `json:"credit"`
	Peaks      []float32 `json:"peaks"`
}

// kidsFactDTO é um fato do conteúdo Kids, só com o texto.
type kidsFactDTO struct {
	Text string `json:"text"`
}

// kidsDTO é o conteúdo para crianças (frente B). Não tem nenhuma URL: nenhum
// link chega à jornada Kids.
type kidsDTO struct {
	Description string        `json:"description"`
	Facts       []kidsFactDTO `json:"facts"`
	DidYouKnow  string        `json:"didYouKnow"`
}

// clusterDTO é um grupo de avistamentos do mapa.
type clusterDTO struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Count     int     `json:"count"`
	Precision float64 `json:"precision"`
}

// whereToFindDTO é a seção "Onde encontrar": UFs, biomas e pontos do mapa.
type whereToFindDTO struct {
	States   []string     `json:"states"`
	Biomes   []string     `json:"biomes"`
	Clusters []clusterDTO `json:"clusters"`
}

// speciesDTO é a espécie completa da tela de detalhe. chips são os biomas
// com rótulo, mostrados como etiquetas.
type speciesDTO struct {
	ID                 string         `json:"id"`
	CommonName         string         `json:"commonName"`
	ScientificName     string         `json:"scientificName"`
	Family             string         `json:"family"`
	ConservationStatus *labelDTO      `json:"conservationStatus"`
	SizeCm             *int           `json:"sizeCm"`
	Diet               *string        `json:"diet"`
	Description        string         `json:"description"`
	DescriptionCredit  creditDTO      `json:"descriptionCredit"`
	Facts              []factDTO      `json:"facts"`
	Chips              []labelDTO     `json:"chips"`
	Photos             []photoDTO     `json:"photos"`
	Audio              *audioDTO      `json:"audio"`
	WhereToFind        whereToFindDTO `json:"whereToFind"`
	PrimaryBiome       *string        `json:"primaryBiome"`
	Kids               *kidsDTO       `json:"kids"` // null = a ave não entra no Kids
}

// biomeCountDTO e stateCountDTO são as opções de filtro com as contagens.
type biomeCountDTO struct {
	Code         string `json:"code"`
	Label        string `json:"label"`
	SpeciesCount int    `json:"speciesCount"`
}

type stateCountDTO struct {
	Code         string `json:"code"`
	SpeciesCount int    `json:"speciesCount"`
}

// filtersDTO é a resposta de /v1/filters. primaryBiomes conta por bioma
// principal, no mesmo formato de biomes: é a conta do álbum do Kids.
type filtersDTO struct {
	Biomes        []biomeCountDTO `json:"biomes"`
	States        []stateCountDTO `json:"states"`
	PrimaryBiomes []biomeCountDTO `json:"primaryBiomes"`
}

// featuredDTO é a resposta de /v1/featured: a data (AAAA-MM-DD, fuso de São
// Paulo), o card da ave e o canto (null se a ave não tiver).
type featuredDTO struct {
	Date    string     `json:"date"`
	Species summaryDTO `json:"species"`
	Audio   *audioDTO  `json:"audio"`
}

// conservationDTO devolve o código com o rótulo, ou nil para status
// desconhecido (o app não mostra selo).
func conservationDTO(s domain.ConservationStatus) *labelDTO {
	label, ok := conservationLabels[s]
	if !ok {
		return nil // desconhecido → null
	}
	return &labelDTO{Code: string(s), Label: label}
}

// toCredit copia o crédito do domínio para o DTO.
func toCredit(c domain.Credit) creditDTO {
	return creditDTO{Author: c.Author, License: c.License, Source: c.Source, SourceURL: c.SourceURL}
}

// biomeCode devolve o código do bioma, ou nil para "" (sem bioma): no JSON,
// null, nunca uma string vazia nem "unspecified".
func biomeCode(b domain.Biome) *string {
	if b == "" {
		return nil
	}
	s := string(b)
	return &s
}

// toSummaryDTO monta o card de uma ave, já com a URL da miniatura.
func toSummaryDTO(ctx context.Context, s domain.Summary, m *media.Builder) summaryDTO {
	return summaryDTO{ID: s.ID, CommonName: s.CommonName, ScientificName: s.ScientificName,
		ThumbnailURL: m.URL(ctx, s.ThumbnailKey), ConservationStatus: conservationDTO(s.Conservation), PrimaryBiome: biomeCode(s.PrimaryBiome)}
}

// toAudioDTO monta o canto. Sem canto, ou com uma chave que não vira URL
// segura, devolve nil: o app esconde o player, em vez de receber uma URL
// montada a partir de uma chave vazia.
func toAudioDTO(ctx context.Context, a *domain.Audio, m *media.Builder) *audioDTO {
	if a == nil {
		return nil
	}
	u := m.URL(ctx, a.Key)
	if u == nil {
		return nil
	}
	return &audioDTO{URL: *u, DurationMs: a.DurationMs, Credit: toCredit(a.Credit), Peaks: append(make([]float32, 0, len(a.Peaks)), a.Peaks...)}
}

// toPageDTO monta a página, já trocando as chaves de mídia por URLs.
func toPageDTO(ctx context.Context, p domain.SpeciesPage, m *media.Builder) speciesPageDTO {
	out := speciesPageDTO{Items: make([]summaryDTO, 0, len(p.Items))}
	for _, s := range p.Items {
		out.Items = append(out.Items, toSummaryDTO(ctx, s, m))
	}
	if p.NextCursor != "" {
		out.NextCursor = &p.NextCursor
	}
	return out
}

// toSpeciesDTO monta a espécie. As listas são criadas com make, mesmo
// vazias, para saírem como [] no JSON, e não como null: o app pode percorrê-
// las sem conferir null.
func toSpeciesDTO(ctx context.Context, s domain.Species, m *media.Builder) speciesDTO {
	out := speciesDTO{
		ID: s.ID, CommonName: s.CommonName, ScientificName: s.ScientificName, Family: s.Family,
		ConservationStatus: conservationDTO(s.Conservation), SizeCm: s.SizeCm, Diet: s.Diet,
		Description: s.Description, DescriptionCredit: toCredit(s.DescriptionCredit),
		Audio: toAudioDTO(ctx, s.Audio, m), PrimaryBiome: biomeCode(s.PrimaryBiome),
		Facts: make([]factDTO, 0, len(s.Facts)), Chips: make([]labelDTO, 0, len(s.Biomes)), Photos: make([]photoDTO, 0, len(s.Photos)),
		WhereToFind: whereToFindDTO{States: make([]string, 0, len(s.States)), Biomes: make([]string, 0, len(s.Biomes)), Clusters: make([]clusterDTO, 0, len(s.Clusters))},
	}
	for _, f := range s.Facts {
		out.Facts = append(out.Facts, factDTO{Text: f.Text, Source: f.Source})
	}
	for _, b := range s.Biomes {
		out.Chips = append(out.Chips, labelDTO{Code: string(b), Label: biomeLabels[b]})
		out.WhereToFind.Biomes = append(out.WhereToFind.Biomes, string(b))
	}
	out.WhereToFind.States = append(out.WhereToFind.States, s.States...)
	for _, p := range s.Photos {
		out.Photos = append(out.Photos, photoDTO{ThumbURL: m.URL(ctx, p.ThumbKey), MediumURL: m.URL(ctx, p.MediumKey), LargeURL: m.URL(ctx, p.LargeKey),
			Width: p.Width, Height: p.Height, Credit: toCredit(p.Credit)})
	}
	if k := s.Kids; k != nil {
		out.Kids = &kidsDTO{Description: k.Description, DidYouKnow: k.DidYouKnow, Facts: make([]kidsFactDTO, 0, len(k.Facts))}
		for _, f := range k.Facts {
			out.Kids.Facts = append(out.Kids.Facts, kidsFactDTO{Text: f})
		}
	}
	for _, c := range s.Clusters {
		out.WhereToFind.Clusters = append(out.WhereToFind.Clusters, clusterDTO{Lat: c.Lat, Lng: c.Lng, Count: c.Count, Precision: c.Precision})
	}
	return out
}

// toFiltersDTO monta os filtros, com o rótulo de cada bioma.
func toFiltersDTO(f domain.Filters) filtersDTO {
	out := filtersDTO{Biomes: toBiomeCounts(f.Biomes), States: make([]stateCountDTO, 0, len(f.States)), PrimaryBiomes: toBiomeCounts(f.PrimaryBiomes)}
	for _, s := range f.States {
		out.States = append(out.States, stateCountDTO{Code: s.State, SpeciesCount: s.SpeciesCount})
	}
	return out
}

// toBiomeCounts monta as contagens por bioma com o rótulo; sempre uma lista
// ([] e não null).
func toBiomeCounts(counts []domain.BiomeCount) []biomeCountDTO {
	out := make([]biomeCountDTO, 0, len(counts))
	for _, b := range counts {
		out = append(out, biomeCountDTO{Code: string(b.Biome), Label: biomeLabels[b.Biome], SpeciesCount: b.SpeciesCount})
	}
	return out
}

// toFeaturedDTO monta a resposta da "Ave do dia".
func toFeaturedDTO(ctx context.Context, out usecase.FeaturedOutput, m *media.Builder) featuredDTO {
	return featuredDTO{Date: out.Date, Species: toSummaryDTO(ctx, out.Featured.Species, m), Audio: toAudioDTO(ctx, out.Featured.Audio, m)}
}
