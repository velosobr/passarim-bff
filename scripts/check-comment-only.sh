#!/usr/bin/env bash
# Verifica que uma branch só mudou COMENTÁRIOS (e linhas em branco) em relação
# a uma base. Serve às branches que comentam o código do Passarim para
# estudo sem alterar nenhuma linha de código.
#
# Uso: bash scripts/check-comment-only.sh [base]   (base padrão: origin/main)
# Sai com 0 quando está tudo certo; com 1 quando alguma linha alterada é código,
# e imprime "arquivo: linha" de cada violação.
#
# A decisão é por linha do "git diff": cada linha adicionada (+) ou removida (-)
# precisa ser vazia ou um comentário na sintaxe da linguagem do arquivo.

# -e para no primeiro erro inesperado; -u reclama de variável não definida;
# -o pipefail faz um pipe falhar se qualquer comando dele falhar.
set -euo pipefail

base="${1:-origin/main}"

# Arquivos que podem mudar livremente numa branch de comentários: a configuração de lint que
# os comentários exigirem, o próprio verificador, a CI que o chama e o README.
liberados='^(\.golangci\.yml|config/detekt\.yml|\.editorconfig|README\.md|scripts/check-comment-only(_test)?\.sh|\.github/workflows/ci\.yml)$'

# estilo_do_arquivo <caminho>: escreve qual sintaxe de comentário o arquivo usa.
# Retorna erro para extensões que uma branch de comentários não deveria tocar (por exemplo,
# JSON, que nem tem comentário).
estilo_do_arquivo() {
  case "$(basename "$1")" in
    *.go | *.kt | *.kts | *.swift | *.xcconfig) echo barra ;;
    *.sql) echo sql ;;
    *.yml | *.yaml | *.sh | *.properties | Dockerfile* | Makefile | .gitignore | .dockerignore) echo cerquilha ;;
    *.xml) echo xml ;;
    *) return 1 ;;
  esac
}

# eh_comentario <estilo> <linha>: sucesso (0) se a linha é vazia ou só comentário.
eh_comentario() {
  local estilo="$1" t="$2"
  # Remove os espaços do começo e do fim, para olhar só o conteúdo.
  t="${t#"${t%%[![:space:]]*}"}"
  t="${t%"${t##*[![:space:]]}"}"
  [[ -z "$t" ]] && return 0
  case "$estilo" in
    barra)
      # Diretivas do Go parecem comentário, mas mudam o build ou o lint.
      [[ "$t" == "//go:"* || "$t" == "//nolint"* ]] && return 1
      # Dentro de um bloco /* */, as linhas começam com "*" seguido de espaço
      # (ou são só "*" ou "*/"); "*p = 1" é código (desreferência de ponteiro).
      [[ "$t" == "//"* || "$t" == "/*"* || "$t" == "*" || "$t" == "* "* || "$t" == "*/"* ]] && return 0
      ;;
    sql)
      # "-- name: X :one" é a marcação que o sqlc usa para gerar código.
      [[ "$t" == "-- name:"* ]] && return 1
      [[ "$t" == "--"* ]] && return 0
      ;;
    cerquilha)
      # "#!" escolhe o interpretador e "# syntax=" muda o parser do Dockerfile.
      [[ "$t" == "#!"* || "$t" == "# syntax="* ]] && return 1
      [[ "$t" == "#"* ]] && return 0
      ;;
    xml)
      [[ "$t" == "<!--"*"-->" ]] && return 0
      ;;
  esac
  return 1
}

violacoes=0

# Para cada arquivo que mudou entre a base e o HEAD...
while IFS= read -r arquivo; do
  [[ "$arquivo" =~ $liberados ]] && continue
  if ! estilo="$(estilo_do_arquivo "$arquivo")"; then
    echo "$arquivo: tipo de arquivo que uma branch de comentários não pode alterar"
    violacoes=$((violacoes + 1))
    continue
  fi
  # ...olhamos só as linhas + e - DEPOIS do primeiro "@@". Antes dele ficam os
  # cabeçalhos "--- a/x" e "+++ b/x"; filtrar por "@@" (e não por "---") evita
  # confundir um comentário SQL removido ("-- x" vira "--- x") com cabeçalho.
  while IFS= read -r linha; do
    if ! eh_comentario "$estilo" "${linha:1}"; then
      echo "$arquivo: $linha"
      violacoes=$((violacoes + 1))
    fi
  done < <(git diff -U0 --no-color "$base"...HEAD -- "$arquivo" | awk '/^@@/ { hunk = 1; next } hunk && /^[+-]/')
done < <(git diff --name-only "$base"...HEAD)

if [[ "$violacoes" -gt 0 ]]; then
  echo "$violacoes linha(s) de código alteradas: esta branch só pode mudar comentários"
  exit 1
fi
echo "ok: só comentários e linhas em branco mudaram"
