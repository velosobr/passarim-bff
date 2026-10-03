package cache

import "github.com/velosobr/passarim-bff/internal/domain"

// Estas structs são o FORMATO GRAVADO no Redis (com tags JSON). O domínio não
// tem JSON de propósito: assim renomear um campo do domínio não muda, sem aviso,
// o que está guardado.

type creditJSON struct {
	Author    string `json:"author"`
	License   string `json:"license"`
	Source    string `json:"source"`
	SourceURL string `json:"sourceUrl"`
}

type summaryJSON struct {
	ID           string `json:"id"`
	Scientific   string `json:"scientificName"`
	Common       string `json:"commonName"`
	ThumbnailKey string `json:"thumbnailKey"`
	Conservation string `json:"conservation"`
}

type pageJSON struct {
	Items      []summaryJSON `json:"items"`
	NextCursor string        `json:"nextCursor"`
}

type photoJSON struct {
	Thumb  string     `json:"thumbKey"`
	Medium string     `json:"mediumKey"`
	Large  string     `json:"largeKey"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Credit creditJSON `json:"credit"`
}

type audioJSON struct {
	Key        string     `json:"key"`
	DurationMs int        `json:"durationMs"`
	Credit     creditJSON `json:"credit"`
}

type factJSON struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type clusterJSON struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Count     int     `json:"count"`
	Precision float64 `json:"precision"`
}

type speciesJSON struct {
	ID           string        `json:"id"`
	Scientific   string        `json:"scientificName"`
	Common       string        `json:"commonName"`
	Family       string        `json:"family"`
	SizeCm       *int          `json:"sizeCm"`
	Diet         *string       `json:"diet"`
	Conservation string        `json:"conservation"`
	Description  string        `json:"description"`
	DescCredit   creditJSON    `json:"descriptionCredit"`
	Facts        []factJSON    `json:"facts"`
	Biomes       []string      `json:"biomes"`
	States       []string      `json:"states"`
	Photos       []photoJSON   `json:"photos"`
	Audio        *audioJSON    `json:"audio"`
	Clusters     []clusterJSON `json:"clusters"`
}

type filtersJSON struct {
	Biomes []struct {
		Biome string `json:"biome"`
		Count int    `json:"count"`
	} `json:"biomes"`
	States []struct {
		State string `json:"state"`
		Count int    `json:"count"`
	} `json:"states"`
}

func creditTo(c domain.Credit) creditJSON {
	return creditJSON{c.Author, c.License, c.Source, c.SourceURL}
}
func creditFrom(c creditJSON) domain.Credit {
	return domain.Credit{Author: c.Author, License: c.License, Source: c.Source, SourceURL: c.SourceURL}
}

func pageTo(p domain.SpeciesPage) pageJSON {
	out := pageJSON{NextCursor: p.NextCursor, Items: make([]summaryJSON, 0, len(p.Items))}
	for _, s := range p.Items {
		out.Items = append(out.Items, summaryJSON{s.ID, s.ScientificName, s.CommonName, s.ThumbnailKey, string(s.Conservation)})
	}
	return out
}

func pageFrom(p pageJSON) domain.SpeciesPage {
	out := domain.SpeciesPage{NextCursor: p.NextCursor, Items: make([]domain.Summary, 0, len(p.Items))}
	for _, s := range p.Items {
		out.Items = append(out.Items, domain.Summary{ID: s.ID, ScientificName: s.Scientific, CommonName: s.Common, ThumbnailKey: s.ThumbnailKey, Conservation: domain.ConservationStatus(s.Conservation)})
	}
	return out
}

func speciesTo(s domain.Species) speciesJSON {
	out := speciesJSON{ID: s.ID, Scientific: s.ScientificName, Common: s.CommonName, Family: s.Family, SizeCm: s.SizeCm, Diet: s.Diet,
		Conservation: string(s.Conservation), Description: s.Description, DescCredit: creditTo(s.DescriptionCredit), States: s.States}
	for _, f := range s.Facts {
		out.Facts = append(out.Facts, factJSON{f.Text, f.Source})
	}
	for _, b := range s.Biomes {
		out.Biomes = append(out.Biomes, string(b))
	}
	for _, p := range s.Photos {
		out.Photos = append(out.Photos, photoJSON{p.ThumbKey, p.MediumKey, p.LargeKey, p.Width, p.Height, creditTo(p.Credit)})
	}
	if s.Audio != nil {
		out.Audio = &audioJSON{s.Audio.Key, s.Audio.DurationMs, creditTo(s.Audio.Credit)}
	}
	for _, c := range s.Clusters {
		out.Clusters = append(out.Clusters, clusterJSON{c.Lat, c.Lng, c.Count, c.Precision})
	}
	return out
}

func speciesFrom(s speciesJSON) domain.Species {
	out := domain.Species{ID: s.ID, ScientificName: s.Scientific, CommonName: s.Common, Family: s.Family, SizeCm: s.SizeCm, Diet: s.Diet,
		Conservation: domain.ConservationStatus(s.Conservation), Description: s.Description, DescriptionCredit: creditFrom(s.DescCredit), States: s.States}
	for _, f := range s.Facts {
		out.Facts = append(out.Facts, domain.Fact{Text: f.Text, Source: f.Source})
	}
	for _, b := range s.Biomes {
		out.Biomes = append(out.Biomes, domain.Biome(b))
	}
	for _, p := range s.Photos {
		out.Photos = append(out.Photos, domain.Photo{ThumbKey: p.Thumb, MediumKey: p.Medium, LargeKey: p.Large, Width: p.Width, Height: p.Height, Credit: creditFrom(p.Credit)})
	}
	if s.Audio != nil {
		out.Audio = &domain.Audio{Key: s.Audio.Key, DurationMs: s.Audio.DurationMs, Credit: creditFrom(s.Audio.Credit)}
	}
	for _, c := range s.Clusters {
		out.Clusters = append(out.Clusters, domain.Cluster{Lat: c.Lat, Lng: c.Lng, Count: c.Count, Precision: c.Precision})
	}
	return out
}

func filtersTo(f domain.Filters) filtersJSON {
	var out filtersJSON
	for _, b := range f.Biomes {
		out.Biomes = append(out.Biomes, struct {
			Biome string `json:"biome"`
			Count int    `json:"count"`
		}{string(b.Biome), b.SpeciesCount})
	}
	for _, s := range f.States {
		out.States = append(out.States, struct {
			State string `json:"state"`
			Count int    `json:"count"`
		}{s.State, s.SpeciesCount})
	}
	return out
}

func filtersFrom(f filtersJSON) domain.Filters {
	var out domain.Filters
	for _, b := range f.Biomes {
		out.Biomes = append(out.Biomes, domain.BiomeCount{Biome: domain.Biome(b.Biome), SpeciesCount: b.Count})
	}
	for _, s := range f.States {
		out.States = append(out.States, domain.StateCount{State: s.State, SpeciesCount: s.Count})
	}
	return out
}
