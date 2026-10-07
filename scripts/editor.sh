#!/bin/sh
# Costruisce l'editor dell'admin (web/static/vendor/editor.{js,css}) con Node in
# container: Node non serve sul PC. Senza, l'admin usa la textarea normale.
# Uso (da Git Bash su Windows anteporre MSYS_NO_PATHCONV=1): sh scripts/editor.sh
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
if command -v cygpath >/dev/null 2>&1; then root=$(cygpath -m "$root"); fi
mkdir -p "$root/web/static/vendor"
docker run --rm \
	-v "$root/web/editor":/src:ro \
	-v "$root/web/static/vendor":/out \
	node:24-alpine sh -ec '
		cp -r /src /w && cd /w
		corepack enable
		CI=true pnpm install --frozen-lockfile
		OUT_DIR=/out pnpm build
	'
