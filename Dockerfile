# Build em dois estágios: o Go completo compila; a imagem final leva SÓ o binário.
# Estágio 1 ("build"): imagem com o Go para compilar.
FROM golang:1.27-alpine AS build
WORKDIR /src
# Copiar go.mod/go.sum primeiro aproveita o cache do Docker.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0 gera binário estático.
# -trimpath tira os caminhos da máquina de build do binário; -ldflags "-s -w"
# remove a tabela de símbolos e a informação de depuração, deixando o
# binário menor.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bff ./cmd/bff

# distroless: sem shell nem gerenciador de pacotes; "nonroot" = não roda como root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bff /usr/local/bin/bff
USER nonroot:nonroot
# 8080 é a API; 9091, as métricas (só na rede interna).
EXPOSE 8080 9091
ENTRYPOINT ["/usr/local/bin/bff"]
