#!/bin/sh
set -eu

repo="${MAC_SCHEDULE_REPO:-avyayv/mac-schedule}"
version="${MAC_SCHEDULE_VERSION:-latest}"
install_dir="${MAC_SCHEDULE_INSTALL_DIR:-$HOME/.local/bin}"
bin_name="${MAC_SCHEDULE_BIN:-schedule}"
from_source="${MAC_SCHEDULE_FROM_SOURCE:-0}"

usage() {
  cat <<'EOF'
Install mac-schedule.

Usage:
  curl -fsSL https://raw.githubusercontent.com/avyayv/mac-schedule/main/install.sh | sh
  ./install.sh [--dir DIR] [--version VERSION] [--repo OWNER/REPO] [--bin NAME]
  ./install.sh --from-source [--dir DIR]

Environment:
  MAC_SCHEDULE_INSTALL_DIR   install directory (default: ~/.local/bin)
  MAC_SCHEDULE_VERSION       release tag to install (default: latest)
  MAC_SCHEDULE_REPO          GitHub repo (default: avyayv/mac-schedule)
  MAC_SCHEDULE_BIN           installed binary name (default: schedule)
  MAC_SCHEDULE_FROM_SOURCE   set to 1 to build the local checkout
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dir)
      [ "$#" -ge 2 ] || { echo "--dir requires a value" >&2; exit 2; }
      install_dir="$2"
      shift 2
      ;;
    --version)
      [ "$#" -ge 2 ] || { echo "--version requires a value" >&2; exit 2; }
      version="$2"
      shift 2
      ;;
    --repo)
      [ "$#" -ge 2 ] || { echo "--repo requires a value" >&2; exit 2; }
      repo="$2"
      shift 2
      ;;
    --bin)
      [ "$#" -ge 2 ] || { echo "--bin requires a value" >&2; exit 2; }
      bin_name="$2"
      shift 2
      ;;
    --from-source)
      from_source=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

mkdir -p "$install_dir"
out="$install_dir/$bin_name"

if [ "$from_source" = "1" ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "go is required for --from-source" >&2
    exit 1
  fi
  script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  cd "$script_dir"
  go build -ldflags "-s -w -X main.version=dev -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o "$out" .
  echo "installed $out"
  exit 0
fi

os=$(uname -s)
case "$os" in
  Darwin) os="Darwin" ;;
  *) echo "mac-schedule only supports macOS (got $os)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="x86_64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

archive="schedule_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$version"
tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM

fetch() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$dest"
  else
    echo "curl or wget is required" >&2
    exit 1
  fi
}

echo "downloading $repo@$version for $os/$arch"
fetch "$base_url/$archive" "$tmp/$archive"

if fetch "$base_url/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  if command -v shasum >/dev/null 2>&1; then
    checksum_line=$(grep "  $archive$" "$tmp/checksums.txt" || true)
    if [ -z "$checksum_line" ]; then
      echo "checksum for $archive not found" >&2
      exit 1
    fi
    (cd "$tmp" && printf '%s\n' "$checksum_line" | shasum -a 256 -c - >/dev/null)
  else
    echo "warning: shasum not found; skipping checksum verification" >&2
  fi
fi

tar -xzf "$tmp/$archive" -C "$tmp"
chmod +x "$tmp/schedule"
mv "$tmp/schedule" "$out"

echo "installed $out"
echo "run '$out version' to verify"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "note: add $install_dir to PATH to run '$bin_name' directly" ;;
esac
