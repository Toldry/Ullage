#!/usr/bin/env bash
# Writes THIRD-PARTY-NOTICES.txt for a release: the license of every Go module linked
# into the Windows executable, the Go standard library, the browser runtime dependencies
# bundled into the UI, and the embedded IBM Plex fonts. The list comes from the build
# graph, so it cannot drift from what actually ships. Usage: scripts/notices.sh [output]
set -euo pipefail
cd "$(dirname "$0")/.."
output="${1:-THIRD-PARTY-NOTICES.txt}"

section() {
  printf '%s\n%s\n%s\n\n' "================================================================" \
    "$1" "================================================================"
}

license_file() {
  for name in LICENSE LICENSE.md LICENSE.txt COPYING; do
    if [[ -f "$1/$name" ]]; then echo "$1/$name"; return 0; fi
  done
  echo "no license file found in $1" >&2
  return 1
}

{
  echo "Ullage includes the following third-party software."
  echo
  section "Go standard library ($(go env GOVERSION))"
  cat "$(license_file "$(go env GOROOT)")"
  echo
  GOOS=windows GOARCH=amd64 go list -deps \
    -f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}} {{.Dir}}{{end}}{{end}}' \
    ./cmd/ullage | sort -u | while read -r module directory; do
    section "$module"
    cat "$(license_file "$directory")"
    echo
  done
  for dependency in $(node -e \
    'console.log(Object.keys(require("./web/package.json").dependencies || {}).join(" "))'); do
    version=$(node -e "console.log(require('./web/node_modules/$dependency/package.json').version)")
    section "$dependency@$version (npm, bundled into the user interface)"
    cat "$(license_file "web/node_modules/$dependency")"
    echo
  done
  section "IBM Plex Sans and IBM Plex Mono (fonts embedded in the user interface)"
  cat web/src/fonts/OFL-LICENSE.txt
} > "$output"
echo "$output"
