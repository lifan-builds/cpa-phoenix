#!/usr/bin/env bash
set -euo pipefail
version="${1:-0.1.0}"
CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -buildmode=c-shared -o cpa-phoenix.dylib .
archive="cpa-phoenix-${version}.zip"
tmp_archive="${archive}.tmp.$$"
trap 'rm -f "$tmp_archive"' EXIT
rm -f "$tmp_archive"
(cd "$(dirname "$archive")" && zip -q "$(basename "$tmp_archive")" "$(basename cpa-phoenix.dylib)" README.md LICENSE NOTICE)
mv -f "$tmp_archive" "$archive"
shasum -a 256 "$archive" > "${archive}.sha256"
