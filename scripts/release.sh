#!/usr/bin/env bash
# Cria uma tag de versão e dispara o release (CI roda goreleaser na tag).
#
# Uso: scripts/release.sh v1.2.3
set -euo pipefail

tag="${1:-}"
if [[ -z "$tag" ]]; then
  echo "Uso: $0 vX.Y.Z" >&2
  exit 1
fi
if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
  echo "Tag deve seguir o formato vX.Y.Z (ex.: v1.2.3)" >&2
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "Árvore de trabalho suja; faça commit antes de releasar." >&2
  exit 1
fi

echo "==> Rodando testes"
go test ./...

echo "==> Criando tag $tag"
git tag -a "$tag" -m "Release $tag"
git push origin "$tag"

echo "Tag $tag enviada. O workflow de CI fará o release via goreleaser."
