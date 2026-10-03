# Build em dois estágios: o Go completo compila; a imagem final leva SÓ o binário.
FROM golang:1.27-alpine AS build
WORKDIR /src
# Copiar go.mod/go.sum primeiro aproveita o cache do Docker.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0 gera binário estático.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bff ./cmd/bff

# distroless: sem shell nem gerenciador de pacotes; "nonroot" = não roda como root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bff /usr/local/bin/bff
USER nonroot:nonroot
EXPOSE 8080 9091
ENTRYPOINT ["/usr/local/bin/bff"]
