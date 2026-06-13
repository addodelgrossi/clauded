#!/usr/bin/env bash
# Instala o cloudflared no macOS (Homebrew) ou Linux (pacote oficial).
set -euo pipefail

if command -v cloudflared >/dev/null 2>&1; then
  echo "cloudflared já instalado: $(cloudflared --version)"
  exit 0
fi

os="$(uname -s)"
case "$os" in
  Darwin)
    if ! command -v brew >/dev/null 2>&1; then
      echo "Homebrew não encontrado. Instale em https://brew.sh e tente de novo." >&2
      exit 1
    fi
    echo "==> Instalando cloudflared via Homebrew"
    brew install cloudflared
    ;;
  Linux)
    arch="$(uname -m)"
    case "$arch" in
      x86_64)  pkg_arch="amd64" ;;
      aarch64) pkg_arch="arm64" ;;
      armv7l)  pkg_arch="arm" ;;
      *) echo "Arquitetura não suportada: $arch" >&2; exit 1 ;;
    esac
    url="https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${pkg_arch}"
    echo "==> Baixando cloudflared ($pkg_arch)"
    sudo curl -fsSL "$url" -o /usr/local/bin/cloudflared
    sudo chmod +x /usr/local/bin/cloudflared
    ;;
  *)
    echo "SO não suportado por este script: $os" >&2
    exit 1
    ;;
esac

echo "Pronto: $(cloudflared --version)"
