#!/usr/bin/env sh
# Instalador one-liner do clauded.
#
#   curl -fsSL https://raw.githubusercontent.com/addodelgrossi/clauded/main/scripts/install.sh | sh
#
# Variáveis de ambiente opcionais:
#   CLAUDED_VERSION   versão a instalar (ex.: v1.2.3). Default: última release.
#   CLAUDED_INSTALL_DIR  diretório de instalação. Default: ~/.local/bin
#                        (ou /usr/local/bin se ~/.local/bin não estiver no PATH).
set -eu

REPO="addodelgrossi/clauded"
BINARY="clauded"

info()  { printf '\033[1;34m==>\033[0m %s\n' "$1"; }
err()   { printf '\033[1;31merro:\033[0m %s\n' "$1" >&2; exit 1; }

# --- detecta OS/arch ---
os="$(uname -s)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) err "SO não suportado por este script: $os (no Windows, baixe o .zip da página de releases)" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "arquitetura não suportada: $arch" ;;
esac

# --- ferramenta de download ---
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1"; }
  dlo() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO- "$1"; }
  dlo() { wget -qO "$2" "$1"; }
else
  err "curl ou wget são necessários"
fi

# --- resolve a versão ---
version="${CLAUDED_VERSION:-}"
if [ -z "$version" ]; then
  info "Buscando a última release de $REPO"
  version="$(dl "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [ -n "$version" ] || err "não foi possível determinar a última versão"
fi
# Os archives do goreleaser usam a versão sem o 'v' inicial.
ver_noprefix="${version#v}"

archive="${BINARY}_${ver_noprefix}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$version/$archive"

# --- diretório de instalação ---
install_dir="${CLAUDED_INSTALL_DIR:-}"
if [ -z "$install_dir" ]; then
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) install_dir="$HOME/.local/bin" ;;
    *) install_dir="/usr/local/bin" ;;
  esac
fi

# --- baixa e extrai ---
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
info "Baixando $archive ($version)"
dlo "$url" "$tmp/$archive" || err "falha ao baixar $url"
tar -xzf "$tmp/$archive" -C "$tmp" "$BINARY" || err "falha ao extrair $BINARY do archive"

# --- instala (usa sudo se necessário) ---
info "Instalando em $install_dir"
if [ -w "$install_dir" ] || mkdir -p "$install_dir" 2>/dev/null; then
  install -m 0755 "$tmp/$BINARY" "$install_dir/$BINARY"
else
  info "Sem permissão de escrita; usando sudo"
  sudo install -m 0755 "$tmp/$BINARY" "$install_dir/$BINARY"
fi

info "clauded $version instalado em $install_dir/$BINARY"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf '\033[1;33mAtenção:\033[0m %s não está no PATH. Adicione: export PATH="%s:$PATH"\n' "$install_dir" "$install_dir" ;;
esac
"$install_dir/$BINARY" --version 2>/dev/null || true
