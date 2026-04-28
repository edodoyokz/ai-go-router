#!/usr/bin/env sh
set -eu

REPO="${ROUTER_REPO:-edodoyokz/ai-go-router}"
INSTALL_DIR="${ROUTER_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${ROUTER_VERSION:-latest}"
SKIP_CHECKSUM="${ROUTER_SKIP_CHECKSUM:-0}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '%s\n' "error: required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf linux ;;
    Darwin) printf darwin ;;
    *) printf '%s\n' "error: unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf amd64 ;;
    arm64|aarch64) printf arm64 ;;
    *) printf '%s\n' "error: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

download() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 20 -o "$out" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -O "$out" "$url"
  else
    printf '%s\n' "error: curl or wget is required" >&2
    exit 1
  fi
}

latest_tag() {
  if [ "$VERSION" != "latest" ]; then
    printf '%s' "$VERSION"
    return
  fi
  need sed
  tmp_json="$TMPDIR/router-release.json"
  download "https://api.github.com/repos/$REPO/releases/latest" "$tmp_json" >/dev/null 2>&1
  sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp_json" | sed -n '1p'
}

verify_checksum() {
  file="$1"
  checksums="$2"
  name="$(basename "$file")"

  if [ ! -s "$checksums" ]; then
    if [ "$SKIP_CHECKSUM" = "1" ]; then
      printf '%s\n' "warning: SHA256SUMS not available; continuing because ROUTER_SKIP_CHECKSUM=1" >&2
      return
    fi
    printf '%s\n' "error: SHA256SUMS not available. Set ROUTER_SKIP_CHECKSUM=1 to continue without verification." >&2
    exit 1
  fi

  line="$(grep "  $name$\|\*$name$\| $name$" "$checksums" || true)"
  if [ -z "$line" ]; then
    if [ "$SKIP_CHECKSUM" = "1" ]; then
      printf '%s\n' "warning: no checksum entry for $name; continuing because ROUTER_SKIP_CHECKSUM=1" >&2
      return
    fi
    printf '%s\n' "error: no checksum entry for $name" >&2
    exit 1
  fi

  expected="$(printf '%s\n' "$line" | sed -n '1s/[[:space:]].*//p')"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file" | sed 's/[[:space:]].*//')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file" | sed 's/[[:space:]].*//')"
  else
    if [ "$SKIP_CHECKSUM" = "1" ]; then
      printf '%s\n' "warning: no checksum tool found; continuing because ROUTER_SKIP_CHECKSUM=1" >&2
      return
    fi
    printf '%s\n' "error: sha256sum or shasum is required for checksum verification" >&2
    exit 1
  fi

  if [ "$expected" != "$actual" ]; then
    printf '%s\n' "error: checksum mismatch for $name" >&2
    exit 1
  fi
}

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

OS="$(detect_os)"
ARCH="$(detect_arch)"
TAG="$(latest_tag)"
if [ -z "$TAG" ]; then
  printf '%s\n' "error: could not resolve release tag" >&2
  exit 1
fi

ASSET="router-$OS-$ARCH"
BASE_URL="https://github.com/$REPO/releases/download/$TAG"
BIN_PATH="$TMPDIR/$ASSET"
SUMS_PATH="$TMPDIR/SHA256SUMS"

printf '%s\n' "Installing router $TAG for $OS/$ARCH"
download "$BASE_URL/SHA256SUMS" "$SUMS_PATH" >/dev/null 2>&1 || true

if download "$BASE_URL/$ASSET" "$BIN_PATH" >/dev/null 2>&1; then
  verify_checksum "$BIN_PATH" "$SUMS_PATH"
else
  ARCHIVE="$TMPDIR/router-$TAG-$OS-$ARCH.tar.gz"
  download "$BASE_URL/router-$TAG-$OS-$ARCH.tar.gz" "$ARCHIVE"
  verify_checksum "$ARCHIVE" "$SUMS_PATH"
  need tar
  tar -xzf "$ARCHIVE" -C "$TMPDIR"
  found="$(find "$TMPDIR" -type f -name "$ASSET" -perm -u+x 2>/dev/null | sed -n '1p')"
  if [ -z "$found" ]; then
    found="$(find "$TMPDIR" -type f -name "$ASSET" 2>/dev/null | sed -n '1p')"
  fi
  if [ -z "$found" ]; then
    printf '%s\n' "error: binary $ASSET not found in archive" >&2
    exit 1
  fi
  cp "$found" "$BIN_PATH"
fi

mkdir -p "$INSTALL_DIR"
install_path="$INSTALL_DIR/router"
cp "$BIN_PATH" "$install_path"
chmod 755 "$install_path"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf '%s\n' "warning: $INSTALL_DIR is not in PATH" >&2 ;;
esac

"$install_path" init

printf '\n%s\n' "Installation complete."
printf '  binary : %s\n' "$install_path"
printf '  start  : router serve\n'
printf '  UI     : http://127.0.0.1:1988\n'
printf '%s\n' "The full login key is stored in ~/.config/router/config.yaml"
