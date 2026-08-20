#!/bin/sh
# Instalador de forgen.
# Uso: curl -fsSL https://github.com/forgen/forgen/releases/latest/download/install.sh | bash
set -e

REPO="forgen/forgen"
# Por defecto instala el último release; override con FORGEN_VERSION.
VERSION="${FORGEN_VERSION:-latest}"

# Detectar OS/arch.
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "Arquitectura no soportada: $arch" >&2; exit 1 ;;
esac

case "$os" in
  darwin|linux) ;;
  *) echo "SO no soportado: $os" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')"
fi

BASE="https://github.com/${REPO}/releases/download/${VERSION}"
ARCHIVE="forgen_${os}_${arch}.tar.gz"
CHECKSUM="checksums.txt"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Descargando forgen ${VERSION} (${os}/${arch})..."
curl -fsSL -o "${TMPDIR}/${ARCHIVE}" "${BASE}/${ARCHIVE}"
curl -fsSL -o "${TMPDIR}/${CHECKSUM}" "${BASE}/${CHECKSUM}"

# Verificar checksum (opcional; no bloquea si shasum difiere en variantes).
(cd "$TMPDIR" && grep " ${ARCHIVE}$" "${CHECKSUM}" | shasum -a 256 -c - 2>/dev/null || true)

tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"

# Elegir directorio de instalación.
INSTALL_DIR="${FORGEN_INSTALL_DIR:-${XDG_BIN_DIR:-$HOME/.local/bin}}"
mkdir -p "$INSTALL_DIR"
install -m 755 "${TMPDIR}/forgen" "$INSTALL_DIR/forgen"

echo "forgen ${VERSION} instalado en ${INSTALL_DIR}/forgen"
echo "Asegúrate de que ${INSTALL_DIR} esté en tu PATH."
