#!/usr/bin/env bash
# Installs the OCR release binary (linux-amd64). No npm, no Node.
#
# Inputs (env):
#   OCR_VERSION    release to install ("latest" or e.g. "1.8.4"); default latest
#   OCR_REPO       source repository; default alibaba/open-code-review
#   OCR_API_URL    API for the latest-release lookup; default the public
#                  https://api.github.com — NOT GITHUB_API_URL, which on
#                  GitHub Enterprise points at a host that has no OCR
#   INSTALL_DIR    where to place the binary; default $RUNNER_TEMP/themis-tools
#
# Support policy: the 3 newest OCR releases are officially supported —
# the window is resolved live from the releases API, never derived by
# version arithmetic. Older pins install with an "unsupported" warning.
set -euo pipefail

OCR_VERSION="${OCR_VERSION:-latest}"
OCR_REPO="${OCR_REPO:-alibaba/open-code-review}"
INSTALL_DIR="${INSTALL_DIR:-${RUNNER_TEMP:-/tmp}/themis-tools}"
API_URL="${OCR_API_URL:-https://api.github.com}"
DOWNLOAD_BASE="${OCR_DOWNLOAD_BASE:-https://github.com/${OCR_REPO}/releases/download}"
ASSET="opencodereview-linux-amd64"

if [ "$OCR_VERSION" = "latest" ]; then
  tag="$(curl -fsSL --connect-timeout 10 --max-time 30 "${API_URL}/repos/${OCR_REPO}/releases/latest" | jq -r '.tag_name // empty')"
  if [ -z "$tag" ]; then
    echo "::error::could not resolve the latest OCR release via ${API_URL}"
    exit 1
  fi
  version="${tag#v}"
else
  version="${OCR_VERSION#v}"
  tag="v${version}"
  # Advisory support-window check: only the 3 newest releases are
  # officially supported. A failed lookup skips the check rather than
  # failing the install — support status never gates anything.
  supported="$(curl -fsSL --connect-timeout 10 --max-time 30 "${API_URL}/repos/${OCR_REPO}/releases?per_page=3" 2>/dev/null | jq -r '.[].tag_name' 2>/dev/null || true)"
  if [ -z "$supported" ]; then
    echo "note: could not resolve the supported OCR release window via ${API_URL}; skipping the support check"
  elif ! printf '%s\n' "$supported" | grep -qxF -e "$tag" -e "$version"; then
    supported_list="$(printf '%s' "$supported" | sed 's/^v//' | tr '\n' ' ' | sed 's/ $//')"
    echo "::warning::ocr-version ${version} is outside the officially supported window (${supported_list// /, }); it may work, but only the 3 newest OCR releases are supported"
  fi
fi

mkdir -p "$INSTALL_DIR"
echo "Installing OCR ${version} (${ASSET})"
if ! curl -fsSL --connect-timeout 10 --max-time 300 "${DOWNLOAD_BASE}/${tag}/${ASSET}" -o "${INSTALL_DIR}/ocr"; then
  echo "::error::failed to download OCR ${version} from ${DOWNLOAD_BASE}/${tag}/${ASSET}"
  exit 1
fi

chmod +x "${INSTALL_DIR}/ocr"
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$INSTALL_DIR" >> "$GITHUB_PATH"
fi
