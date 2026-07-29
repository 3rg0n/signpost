#!/bin/sh
# signpost installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/3rg0n/signpost/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/3rg0n/signpost/main/install.sh | sh -s -- --version v0.1.0
#
# Downloads a tagged release archive and verifies its SHA-256 against the
# checksums.txt published with that release before anything is unpacked. A script
# piped from the network into a shell has no business installing a binary it did not
# check, and "the download completed" is not a check.
#
# POSIX sh on purpose: this runs as whatever /bin/sh is on the machine, which on
# Debian is dash and on Alpine is busybox ash.

set -eu

REPO="3rg0n/signpost"
BIN="signpost"

VERSION=""
INSTALL_DIR=""

usage() {
	cat <<EOF
Install ${BIN} from a tagged GitHub release.

Usage: install.sh [options]

  --version <tag>   install this release (default: the latest release)
  --dir <path>      install into this directory (default: \$HOME/.local/bin,
                    or /usr/local/bin when it is writable)
  -h, --help        show this message

Environment: SIGNPOST_VERSION and SIGNPOST_INSTALL_DIR set the same two values.
EOF
}

die() {
	echo "install.sh: $*" >&2
	exit 1
}

info() { echo "==> $*"; }

# --- arguments ---------------------------------------------------------------

VERSION="${SIGNPOST_VERSION:-}"
INSTALL_DIR="${SIGNPOST_INSTALL_DIR:-}"

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || die "--version needs a tag"
		VERSION="$2"
		shift 2
		;;
	--version=*)
		VERSION="${1#--version=}"
		shift
		;;
	--dir)
		[ $# -ge 2 ] || die "--dir needs a path"
		INSTALL_DIR="$2"
		shift 2
		;;
	--dir=*)
		INSTALL_DIR="${1#--dir=}"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		die "unknown option: $1"
		;;
	esac
done

# --- tools ------------------------------------------------------------------

need() {
	command -v "$1" >/dev/null 2>&1
}

if need curl; then
	# --fail so a 404 is an error rather than an HTML page written to the archive,
	# and --location because a release asset is served as a redirect.
	fetch() { curl -fsSL "$1" -o "$2"; }
	fetch_stdout() { curl -fsSL "$1"; }
elif need wget; then
	fetch() { wget -qO "$2" "$1"; }
	fetch_stdout() { wget -qO- "$1"; }
else
	die "need curl or wget"
fi

need tar || die "need tar"

if need sha256sum; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif need shasum; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	# Refusing rather than installing unverified. An unverified binary from the
	# network is the thing this script exists to avoid handing you.
	die "need sha256sum or shasum to verify the download"
fi

# --- platform ---------------------------------------------------------------

os="$(uname -s)"
case "$os" in
Linux) os="linux" ;;
Darwin) os="darwin" ;;
*) die "unsupported operating system: $os" ;;
esac

arch="$(uname -m)"
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
*) die "unsupported architecture: $arch" ;;
esac

# --- version ----------------------------------------------------------------

if [ -z "$VERSION" ]; then
	info "resolving the latest release"
	# The redirect target of /releases/latest, rather than the API: no rate limit
	# that a shared CI IP will hit, and no JSON to parse in sh.
	if need curl; then
		location="$(curl -fsSI -o /dev/null -w '%{url_effective}' \
			"https://github.com/${REPO}/releases/latest")"
	else
		location="$(wget -qSO /dev/null "https://github.com/${REPO}/releases/latest" 2>&1 |
			awk '/^  Location: /{print $2}' | tail -n1)"
	fi
	VERSION="${location##*/}"
	case "$VERSION" in
	v*) ;;
	*) die "could not determine the latest version; pass --version" ;;
	esac
fi

name="${BIN}_${VERSION}_${os}_${arch}"
archive="${name}.tar.gz"
base="https://github.com/${REPO}/releases/download/${VERSION}"

# --- destination ------------------------------------------------------------

if [ -z "$INSTALL_DIR" ]; then
	# $HOME/.local/bin by default. Installing into /usr/local/bin needs root, and a
	# curl-to-shell script that escalates privilege on its own is a bad habit to
	# teach; it is used only when it is already writable.
	if [ -w /usr/local/bin ] && [ -d /usr/local/bin ]; then
		INSTALL_DIR="/usr/local/bin"
	else
		INSTALL_DIR="${HOME}/.local/bin"
	fi
fi

# --- download and verify ----------------------------------------------------

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t signpost)"
# Removed on every exit path, including a failed verification: a rejected archive
# must not be left behind where someone might unpack it by hand.
trap 'rm -rf "$tmp"' EXIT INT TERM

info "downloading ${archive}"
fetch "${base}/${archive}" "${tmp}/${archive}" ||
	die "no release asset ${archive} for ${VERSION} — check the version and your platform"

info "downloading checksums.txt"
fetch "${base}/checksums.txt" "${tmp}/checksums.txt" ||
	die "release ${VERSION} publishes no checksums.txt; refusing to install unverified"

expected="$(awk -v f="$archive" '$2 == f || $2 == "*" f {print $1}' "${tmp}/checksums.txt")"
[ -n "$expected" ] || die "${archive} is not listed in checksums.txt; refusing to install"

actual="$(sha256 "${tmp}/${archive}")"
if [ "$expected" != "$actual" ]; then
	die "checksum mismatch for ${archive}
  expected ${expected}
  got      ${actual}
Not installing. Either the download was corrupted or the asset is not the one
that was published."
fi
info "sha256 verified"

# --- install ----------------------------------------------------------------

tar -xzf "${tmp}/${archive}" -C "$tmp"
[ -f "${tmp}/${name}/${BIN}" ] || die "archive does not contain ${name}/${BIN}"

mkdir -p "$INSTALL_DIR" || die "cannot create ${INSTALL_DIR}"
chmod 0755 "${tmp}/${name}/${BIN}"
# Copy then rename, so an existing binary is replaced atomically and a running
# process is not writing into the file another one is executing.
cp "${tmp}/${name}/${BIN}" "${INSTALL_DIR}/.${BIN}.new" ||
	die "cannot write to ${INSTALL_DIR}"
mv "${INSTALL_DIR}/.${BIN}.new" "${INSTALL_DIR}/${BIN}"

info "installed ${BIN} ${VERSION} to ${INSTALL_DIR}/${BIN}"

case ":${PATH}:" in
*":${INSTALL_DIR}:"*) ;;
*)
	echo
	echo "${INSTALL_DIR} is not on your PATH. Add it:"
	echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
	;;
esac
