package grpcclient

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	catalogv1 "github.com/velosobr/passarim-proto/gen/go/passarim/catalog/v1"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

var biomeToProto = map[domain.Biome]catalogv1.Biome{
	domain.BiomeAmazonia:      catalogv1.Biome_BIOME_AMAZONIA,
	domain.BiomeMataAtlantica: catalogv1.Biome_BIOME_MATA_ATLANTICA,
	domain.BiomeCerrado:       catalogv1.Biome_BIOME_CERRADO,
	domain.BiomeCaatinga:      catalogv1.Biome_BIOME_CAATINGA,
	domain.BiomePantanal:      catalogv1.Biome_BIOME_PANTANAL,
	domain.BiomePampa:         catalogv1.Biome_BIOME_PAMPA,
}

var biomeFromProto = func() map[catalogv1.Biome]domain.Biome {
	m := make(map[catalogv1.Biome]domain.Biome, len(biomeToProto))
	for d, p := range biomeToProto {
		m[p] = d
	}
	return m
}()

var statusFromProto = map[catalogv1.ConservationStatus]domain.ConservationStatus{
	catalogv1.ConservationStatus_CONSERVATION_STATUS_LC: "LC", catalogv1.ConservationStatus_CONSERVATION_STATUS_NT: "NT",
	catalogv1.ConservationStatus_CONSERVATION_STATUS_VU: "VU", catalogv1.ConservationStatus_CONSERVATION_STATUS_EN: "EN",
	catalogv1.ConservationStatus_CONSERVATION_STATUS_CR: "CR", catalogv1.ConservationStatus_CONSERVATION_STATUS_EW: "EW",
	catalogv1.ConservationStatus_CONSERVATION_STATUS_EX: "EX", catalogv1.ConservationStatus_CONSERVATION_STATUS_DD: "DD",
}

// conservation: UNSPECIFIED (ou valor que não conhecemos) vira "" = desconhecido.
func conservation(s catalogv1.ConservationStatus) domain.ConservationStatus {
	return statusFromProto[s]
}

func credit(c *catalogv1.Credit) domain.Credit {
	return domain.Credit{Author: c.GetAuthor(), License: c.GetLicense(), Source: c.GetSource(), SourceURL: c.GetSourceUrl()}
}

// mapper carrega o logger para avisar (com o request_id) quando descarta dado desconhecido.
type mapper struct {
	ctx context.Context
	log *slog.Logger
}

func (m mapper) warnDropped(what, value string) {
	m.log.WarnContext(m.ctx, "dado desconhecido do catalog descartado", "what", what, "value", value, "request_id", reqmeta.RequestID(m.ctx))
}

func (m mapper) biome(b catalogv1.Biome) (domain.Biome, bool) {
	d, ok := biomeFromProto[b]
	if !ok {
		m.warnDropped("biome", b.String())
	}
	return d, ok
}

func (m mapper) uf(s string) (string, bool) {
	uf, ok := domain.NormalizeUF(s)
	if !ok {
		m.warnDropped("state", s)
	}
	return uf, ok
}

func (m mapper) summary(s *catalogv1.SpeciesSummary) domain.Summary {
	return domain.Summary{ID: s.GetId(), ScientificName: s.GetScientificName(), CommonName: s.GetCommonNamePt(),
		ThumbnailKey: s.GetThumbnailKey(), Conservation: conservation(s.GetConservationStatus())}
}

func (m mapper) species(s *catalogv1.Species) domain.Species {
	out := domain.Species{
		ID: s.GetId(), ScientificName: s.GetScientificName(), CommonName: s.GetCommonNamePt(), Family: s.GetFamily(),
		Conservation: conservation(s.GetConservationStatus()), Description: s.GetDescription(),
		DescriptionCredit: credit(s.GetDescriptionCredit()),
	}
	if s.SizeCm != nil {
		v := int(*s.SizeCm)
		out.SizeCm = &v
	}
	if s.Diet != nil {
		v := *s.Diet
		out.Diet = &v
	}
	for _, f := range s.GetFacts() {
		out.Facts = append(out.Facts, domain.Fact{Text: f.GetText(), Source: f.GetSource()})
	}
	for _, b := range s.GetBiomes() {
		if d, ok := m.biome(b); ok {
			out.Biomes = append(out.Biomes, d)
		}
	}
	for _, st := range s.GetStates() {
		if uf, ok := m.uf(st); ok {
			out.States = append(out.States, uf)
		}
	}
	for _, p := range s.GetPhotos() {
		out.Photos = append(out.Photos, domain.Photo{ThumbKey: p.GetThumbKey(), MediumKey: p.GetMediumKey(), LargeKey: p.GetLargeKey(),
			Width: int(p.GetWidth()), Height: int(p.GetHeight()), Credit: credit(p.GetCredit())})
	}
	if a := s.GetAudio(); a != nil && a.GetKey() != "" { // áudio sem chave = sem áudio
		out.Audio = &domain.Audio{Key: a.GetKey(), DurationMs: int(a.GetDurationMs()), Credit: credit(a.GetCredit())}
	}
	for _, c := range s.GetClusters() {
		out.Clusters = append(out.Clusters, domain.Cluster{Lat: c.GetLat(), Lng: c.GetLng(), Count: int(c.GetCount()), Precision: c.GetPrecision()})
	}
	return out
}

// translateError converte o código gRPC no Kind da tabela única. A causa
// original fica embrulhada (vai para os logs); a mensagem do catalog NUNCA vai ao cliente.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return domain.NewError(domain.KindOf(err), err) // erro local (ex.: contexto)
	}
	var kind domain.Kind
	switch st.Code() {
	case codes.Unavailable:
		kind = domain.KindUnavailable
	case codes.DeadlineExceeded:
		kind = domain.KindTimeout
	case codes.Internal, codes.Unknown, codes.ResourceExhausted:
		kind = domain.KindUpstream
	case codes.NotFound:
		kind = domain.KindNotFound
	case codes.InvalidArgument:
		kind = domain.KindInvalidArgument
	case codes.Canceled:
		kind = domain.KindCanceled
	default: // Unimplemented, PermissionDenied, FailedPrecondition...: erro de versão/config, não queda
		kind = domain.KindInternal
	}
	return domain.NewError(kind, err)
}
