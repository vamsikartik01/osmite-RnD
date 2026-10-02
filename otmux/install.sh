#!/bin/sh
# Installs the latest otmux on Linux or macOS, for the current user (no sudo):
#
#   curl -fsSL https://raw.githubusercontent.com/vamsikartik01/osmite-RnD/main/otmux/install.sh | sh
#
# It downloads otmux from the rolling otmux/latest release, checks it against
# the published SHA-256 checksums, and puts it in ~/.local/bin.
#
# Environment:
#   OTMUX_INSTALL_DIR   install here instead of ~/.local/bin
#   OTMUX_INSTALL_FROM  for testing: install from a local folder holding
#                       otmux-<os>-<arch> and checksums.txt (e.g. dist/ from
#                       release.ps1)

set -eu

base="https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest"
install_dir="${OTMUX_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "otmux install: $*" >&2
	exit 1
}

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "unsupported system $(uname -s); on Windows use install.ps1" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "unsupported processor $(uname -m)" ;;
esac
asset="otmux-$os-$arch"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t otmux-install)
trap 'rm -rf "$tmp"' EXIT INT TERM

# GitHub serves release files from several addresses, and on some networks
# one of them is unreachable. Without a connect timeout curl waits a minute
# on it before trying the next; with one it moves on after a few seconds.
fetch() { # name [progress]
	if [ -n "${OTMUX_INSTALL_FROM:-}" ]; then
		cp "$OTMUX_INSTALL_FROM/$1" "$tmp/$1"
	elif command -v curl >/dev/null 2>&1; then
		show="-sS"
		[ -n "${2:-}" ] && [ -t 2 ] && show="--progress-bar"
		curl -fL $show --connect-timeout 6 --retry 3 --retry-delay 1 -o "$tmp/$1" "$base/$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q --timeout=6 --tries=3 -O "$tmp/$1" "$base/$1"
	else
		fail "needs curl or wget"
	fi
}

echo "Downloading otmux ($os/$arch)..."
fetch "$asset" progress || fail "download of $asset failed"
fetch checksums.txt || fail "download of checksums.txt failed"

# Verify the download. checksums.txt is written on Windows, so drop the CRs.
want=$(tr -d '\r' <"$tmp/checksums.txt" | awk -v f="$asset" '$2 == f || $2 == "*" f { print tolower($1) }')
[ -n "$want" ] || fail "checksums.txt has no entry for $asset"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
else
	fail "needs sha256sum or shasum to check the download"
fi
[ "$want" = "$got" ] || fail "checksum mismatch for $asset (expected $want, got $got)"

# Install. Renaming over the old file is safe while otmux is running: the
# running copy keeps its file, and the new one is used from the next start.
mkdir -p "$install_dir"
chmod 755 "$tmp/$asset"
mv -f "$tmp/$asset" "$install_dir/otmux.new"
mv -f "$install_dir/otmux.new" "$install_dir/otmux"

version=$("$install_dir/otmux" version)
echo "Installed $version to $install_dir/otmux."
case ":$PATH:" in
*":$install_dir:"*) ;;
*)
	echo
	echo "$install_dir isn't on your PATH. Add it, e.g. in ~/.bashrc or ~/.zshrc:"
	echo "  export PATH=\"$install_dir:\$PATH\""
	;;
esac
echo "If otmux was already running, restart it to use the new version: otmux kill-server"
