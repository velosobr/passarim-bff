#!/usr/bin/env bash
# Teste do check-comment-only.sh.
#
# Como funciona: para cada caso, criamos um repositório git descartável numa
# pasta temporária, fazemos um commit "base" com um arquivo, aplicamos uma
# mudança num segundo commit e rodamos o script comparando os dois. Cada caso
# diz se o script deve aprovar (0) ou reprovar (1).
#
# Rodar: bash scripts/check-comment-only_test.sh

# -e para no primeiro erro inesperado; -u reclama de variável não definida;
# -o pipefail faz um pipe falhar se qualquer comando dele falhar.
set -euo pipefail

# Caminho absoluto do script testado, calculado a partir deste arquivo, para o
# teste funcionar de qualquer pasta.
SCRIPT="$(cd "$(dirname "$0")" && pwd)/check-comment-only.sh"
falhas=0

# caso <nome> <arquivo> <conteúdo antes> <conteúdo depois> <saída esperada>
# Monta o repositório temporário, roda o script e compara a saída esperada.
caso() {
  local nome="$1" arquivo="$2" antes="$3" depois="$4" esperado="$5"
  local dir
  dir="$(mktemp -d)"
  (
    cd "$dir"
    git init -q -b main
    git config user.email teste@exemplo.com
    git config user.name teste
    mkdir -p "$(dirname "$arquivo")"
    # Conteúdo vazio no "antes" quer dizer que o arquivo ainda não existe.
    if [[ -n "$antes" ]]; then printf '%s\n' "$antes" >"$arquivo"; else : >.vazio; fi
    git add -A && git commit -q -m base
    git branch base
    # Conteúdo vazio no "depois" quer dizer que o arquivo foi apagado.
    if [[ -n "$depois" ]]; then printf '%s\n' "$depois" >"$arquivo"; else rm -f "$arquivo"; fi
    git add -A && git commit -q -m mudanca
  )
  local obtido=0
  (cd "$dir" && bash "$SCRIPT" base >/dev/null 2>&1) || obtido=$?
  if [[ "$obtido" == "$esperado" ]]; then
    echo "ok    $nome"
  else
    echo "FALHA $nome: esperado $esperado, obtido $obtido"
    falhas=$((falhas + 1))
  fi
  rm -rf "$dir"
}

caso "go: comentário novo"            a/x.go  $'package a\n\nfunc F() {}' $'package a\n\n// F faz algo.\nfunc F() {}' 0
caso "go: bloco /* */ novo"           a/x.go  $'package a' $'/*\n * Cabeçalho.\n */\npackage a' 0
caso "go: linha em branco nova"       a/x.go  $'package a\nfunc F() {}' $'package a\n\nfunc F() {}' 0
caso "go: código alterado"            a/x.go  $'package a\n\nfunc F() {}' $'package a\n\nfunc G() {}' 1
caso "go: comentário no fim da linha" a/x.go  $'package a\n\nfunc F() {}' $'package a\n\nfunc F() {} // F' 1
caso "go: desreferência de ponteiro" a/x.go  $'package a\n\nfunc F(p *int) {\n}' $'package a\n\nfunc F(p *int) {\n*p = 1\n}' 1
caso "go: diretiva go:build"          a/x.go  $'//go:build linux\n\npackage a' $'//go:build darwin\n\npackage a' 1
caso "go: diretiva nolint"            a/x.go  $'package a\n\n//nolint:gosec\nvar x = 1' $'package a\n\n//nolint:errcheck\nvar x = 1' 1
caso "sql: comentário removido"       q.sql   $'-- antigo\nSELECT 1;' $'SELECT 1;' 0
caso "sql: comentário trocado"        q.sql   $'-- antigo\nSELECT 1;' $'-- novo\nSELECT 1;' 0
caso "sql: name do sqlc alterado"     q.sql   $'-- name: A :one\nSELECT 1;' $'-- name: B :one\nSELECT 1;' 1
caso "yaml: comentário novo"          ci.yml  $'a: 1' $'# explica a\na: 1' 0
caso "yaml: valor alterado"           api.yaml $'a: 1' $'a: 2' 1
caso "sh: shebang alterado"           s.sh    $'#!/bin/sh\necho' $'#!/bin/bash\necho' 1
caso "dockerfile: comentário"         Dockerfile $'FROM x' $'# imagem base\nFROM x' 0
caso "dockerfile: diretiva syntax"    Dockerfile $'FROM x' $'# syntax=docker/dockerfile:1\nFROM x' 1
caso "makefile: comentário"           Makefile $'t:\n\tgo test' $'# testes\nt:\n\tgo test' 0
caso "kotlin: kdoc novo"              A.kt    $'package a\n\nclass A' $'package a\n\n/** Um A. */\nclass A' 0
caso "xml: comentário de uma linha"   m.xml   $'<a/>' $'<!-- raiz -->\n<a/>' 0
caso "arquivo novo de código"         n.go    '' $'package n' 1
caso "arquivo de código apagado"      d.go    $'package d' '' 1
caso "arquivo liberado (README)"      README.md $'# x' $'# x\n\nTexto novo.' 0
caso "extensão desconhecida"          x.json  $'{}' $'{"a":1}' 1

# Base que não existe: o script tem que recusar (2) em vez de aprovar sem ter
# comparado nada. Sem a checagem, o "git diff" falha dentro do "< <(...)", que
# o set -e não enxerga, e o laço termina vazio dizendo "ok".
dir="$(mktemp -d)"
(cd "$dir" && git init -q -b main && git -c user.email=t@e.com -c user.name=t commit -q --allow-empty -m base)
obtido=0
(cd "$dir" && bash "$SCRIPT" origin/nao-existe >/dev/null 2>&1) || obtido=$?
if [[ "$obtido" == 2 ]]; then
  echo "ok    base inexistente"
else
  echo "FALHA base inexistente: esperado 2, obtido $obtido"
  falhas=$((falhas + 1))
fi
rm -rf "$dir"

if [[ "$falhas" -gt 0 ]]; then
  echo "$falhas caso(s) falharam"
  exit 1
fi
echo "todos os casos passaram"
