package httpadapter

import (
	"context"

	"github.com/velosobr/passarim-bff/internal/adapter/media"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// DTOs = o contrato REST. São structs PRÓPRIAS: nunca serializamos o domínio
// nem o proto (OWASP API3: só sai o que está listado aqui).

type labelDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type creditDTO struct {
	Author    string `json:"author"`
	License   string `json:"license"`
	Source    string `json:"source"`
	SourceURL string `json:"sourceUrl"`
}

type summaryDTO struct {
	ID                 string    `json:"id"`
	CommonName         string    `json:"commonName"`
	ScientificName     string    `json:"scientificName"`
	ThumbnailURL       *string   `json:"thumbnailUrl"`
	ConservationStatus *labelDTO `json:"conservationStatus"`
}

type speciesPageDTO struct {
	Items      []summaryDTO `json:"items"`
	NextCursor *string      `json:"nextCursor"`
}

type factDTO struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type photoDTO struct {
	ThumbURL  *string   `json:"thumbUrl"`
	MediumURL *string   `json:"mediumUrl"`
	LargeURL  *string   `json:"largeUrl"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Credit    creditDTO `json:"credit"`
}

type audioDTO struct {
	URL        string    `json:"url"`
	DurationMs int       `json:"durationMs"`
	Credit     creditDTO `json:"credit"`
}

type clusterDTO struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Count     int     `json:"count"`
	Precision float64 `json:"precision"`
}

type whereToFindDTO struct {
	States   []string     `json:"states"`
	Biomes   []string     `json:"biomes"`
	Clusters []clusterDTO `json:"clusters"`
}

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
}

type biomeCountDTO struct {
	Code         string `json:"code"`
	Label        string `json:"label"`
	SpeciesCount int    `json:"speciesCount"`
}

type stateCountDTO struct {
	Code         string `json:"code"`
	SpeciesCount int    `json:"speciesCount"`
}

type filtersDTO struct {
	Biomes []biomeCountDTO `json:"biomes"`
	States []stateCountDTO `json:"states"`
}

func conservationDTO(s domain.ConservationStatus) *labelDTO {
	label, ok := conservationLabels[s]
	if !ok {
		return nil // desconhecido → null
	}
	return &labelDTO{Code: string(s), Label: label}
}

func toCredit(c domain.Credit) creditDTO {
	return creditDTO{Author: c.Author, License: c.License, Source: c.Source, SourceURL: c.SourceURL}
}

func toPageDTO(ctx context.Context, p domain.SpeciesPage, m *media.Builder) speciesPageDTO {
	out := speciesPageDTO{Items: make([]summaryDTO, 0, len(p.Items))}
	for _, s := range p.Items {
		out.Items = append(out.Items, summaryDTO{ID: s.ID, CommonName: s.CommonName, ScientificName: s.ScientificName,
			ThumbnailURL: m.URL(ctx, s.ThumbnailKey), ConservationStatus: conservationDTO(s.Conservation)})
	}
	if p.NextCursor != "" {
		out.NextCursor = &p.NextCursor
	}
	return out
}

func toSpeciesDTO(ctx context.Context, s domain.Species, m *media.Builder) speciesDTO {
	out := speciesDTO{
		ID: s.ID, CommonName: s.CommonName, ScientificName: s.ScientificName, Family: s.Family,
		ConservationStatus: conservationDTO(s.Conservation), SizeCm: s.SizeCm, Diet: s.Diet,
		Description: s.Description, DescriptionCredit: toCredit(s.DescriptionCredit),
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
	if s.Audio != nil {
		// Áudio com URL insegura/vazia = sem áudio: o app esconde o player.
		if u := m.URL(ctx, s.Audio.Key); u != nil {
			out.Audio = &audioDTO{URL: *u, DurationMs: s.Audio.DurationMs, Credit: toCredit(s.Audio.Credit)}
		}
	}
	for _, c := range s.Clusters {
		out.WhereToFind.Clusters = append(out.WhereToFind.Clusters, clusterDTO{Lat: c.Lat, Lng: c.Lng, Count: c.Count, Precision: c.Precision})
	}
	return out
}

func toFiltersDTO(f domain.Filters) filtersDTO {
	out := filtersDTO{Biomes: make([]biomeCountDTO, 0, len(f.Biomes)), States: make([]stateCountDTO, 0, len(f.States))}
	for _, b := range f.Biomes {
		out.Biomes = append(out.Biomes, biomeCountDTO{Code: string(b.Biome), Label: biomeLabels[b.Biome], SpeciesCount: b.SpeciesCount})
	}
	for _, s := range f.States {
		out.States = append(out.States, stateCountDTO{Code: s.State, SpeciesCount: s.SpeciesCount})
	}
	return out
}
