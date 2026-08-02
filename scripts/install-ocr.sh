#!/usr/bin/env bash
# Installs the OCR release binary (linux-amd64). No npm, no Node.
#
# Inputs (env):
#   OCR_VERSION    release to install ("latest" or e.g. "1.8.4"); default latest
#   OCR_REPO       source repository; default alibaba/open-code-review
#   INSTALL_DIR    where to place the binary; default $RUNNER_TEMP/themis-tools
#   CHECKSUM_FILE  recorded checksums; default ocr-checksums.txt next to this script
#
# A pinned version must have a recorded checksum and is always
# verified — no recorded hash means no install. "latest" cannot be
# verified (there is nothing trustworthy to compare against ahead of
# time) and installs with a warning.
set -euo pipefail

OCR_VERSION="${OCR_VERSION:-latest}"
OCR_REPO="${OCR_REPO:-alibaba/open-code-review}"
INSTALL_DIR="${INSTALL_DIR:-${RUNNER_TEMP:-/tmp}/themis-tools}"
CHECKSUM_FILE="${CHECKSUM_FILE:-$(dirname "${BASH_SOURCE[0]}")/ocr-checksums.txt}"
API_URL="${GITHUB_API_URL:-https://api.github.com}"
DOWNLOAD_BASE="${OCR_DOWNLOAD_BASE:-https://github.com/${OCR_REPO}/releases/download}"
ASSET="opencodereview-linux-amd64"

compute_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

if [ "$OCR_VERSION" = "latest" ]; then
  echo "::warning::installing the latest OCR release without checksum verification; pin ocr-version for a verified install"
  tag="$(curl -fsSL --connect-timeout 10 --max-time 30 "${API_URL}/repos/${OCR_REPO}/releases/latest" | jq -r '.tag_name // empty')"
  if [ -z "$tag" ]; then
    echo "::error::could not resolve the latest OCR release via ${API_URL}"
    exit 1
  fi
  version="${tag#v}"
else
  version="${OCR_VERSION#v}"
  tag="v${version}"
fi

mkdir -p "$INSTALL_DIR"
echo "Installing OCR ${version} (${ASSET})"
if ! curl -fsSL --connect-timeout 10 --max-time 300 "${DOWNLOAD_BASE}/${tag}/${ASSET}" -o "${INSTALL_DIR}/ocr"; then
  echo "::error::failed to download OCR ${version} from ${DOWNLOAD_BASE}/${tag}/${ASSET}"
  exit 1
fi

if [ "$OCR_VERSION" != "latest" ]; then
  expected="$(awk -v v="$version" '$1 == v {print $2}' "$CHECKSUM_FILE" 2>/dev/null || true)"
  if [ -n "$expected" ]; then
    actual="$(compute_sha256 "${INSTALL_DIR}/ocr")"
    if [ "$actual" != "$expected" ]; then
      echo "::error::OCR ${version} checksum mismatch: expected ${expected}, got ${actual}"
      exit 1
    fi
    echo "Checksum verified for OCR ${version}"
  else
    echo "::error::no checksum recorded for OCR ${version} in ${CHECKSUM_FILE}; refusing to install a pinned version unverified"
    exit 1
  fi
fi

chmod +x "${INSTALL_DIR}/ocr"
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$INSTALL_DIR" >> "$GITHUB_PATH"
fi
