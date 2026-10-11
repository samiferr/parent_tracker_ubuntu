#!/bin/sh
# Regenerates internal/server/web/notices.txt: the licences and notices of the
# third-party material that ships in the binaries and the dashboard. The same
# file is installed as /usr/share/doc/parent-tracker/copyright (NFR-6.11).
#
#   packaging/gen-notices.sh           write the file
#   packaging/gen-notices.sh --check   fail if the file is out of date
set -eu
cd "$(dirname "$0")/.."

out=internal/server/web/notices.txt
vendor=internal/server/web/vendor
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

rule() { printf '\n%s\n' '------------------------------------------------------------------------'; }
section() { rule; printf '%s\n' "$1"; printf '%s\n\n' '------------------------------------------------------------------------'; }

{
cat <<'HEAD'
Parent Tracker: copyright and licence notices

Parent Tracker is a private family project. No licence has been chosen for
its own source code yet, so all rights in it remain with its authors.

This file lists the licences of the third-party material that is built into
the programs or served by the dashboard. Nothing is downloaded from another
site at run time.

Contents
  1. Lumen design system and Adobe Spectrum design tokens
  2. Lucide and Feather icons
  3. React and React DOM
  4. Source Sans 3 and Source Code Pro fonts
  5. The Go standard library
  6. Go modules linked into the programs
HEAD

section '1. Lumen design system and Adobe Spectrum design tokens'
cat <<'EOF1'
The dashboard is built on the Lumen design system, vendored unmodified under
vendor/lumen. Lumen's palette, spacing, radius, type-size and component-height
values are adapted from Adobe's open-source Spectrum design tokens
(@adobe/spectrum-tokens), which are licensed under the Apache License,
Version 2.0. On Debian and Ubuntu the full text is in
/usr/share/common-licenses/Apache-2.0, and at
https://www.apache.org/licenses/LICENSE-2.0

Lumen is an independent system. It is not affiliated with or endorsed by
Adobe, and it uses none of Adobe's marks, fonts or icons.
EOF1

section '2. Lucide and Feather icons'
sed -e '1,/^## Lucide icons/d' "$vendor/lumen/lucide-license.md" | sed -e '/^```$/d'

section '3. React and React DOM'
printf 'react (react.production.min.js)\n\n'; cat "$vendor/react/LICENSE-react"
printf '\nreact-dom (react-dom.production.min.js)\n\n'; cat "$vendor/react/LICENSE-react-dom"

section '4. Source Sans 3 and Source Code Pro fonts'
printf 'Source Sans 3\n\n'; cat "$vendor/fonts/LICENSE-source-sans-3-OFL.txt"
printf '\n\nSource Code Pro\n\n'; cat "$vendor/fonts/LICENSE-source-code-pro-OFL.txt"

section '5. The Go standard library'
cat "$(go env GOROOT)/LICENSE"

section '6. Go modules linked into the programs'
go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/... | sort -u |
while read -r path version dir; do
  printf '%s %s\n\n' "$path" "$version"
  for f in LICENSE LICENSE-GO LICENSE-MMAP-GO LICENSE-LOGO PATENTS; do
    if [ -f "$dir/$f" ]; then
      [ "$f" = LICENSE ] || printf '(%s)\n\n' "$f"
      cat "$dir/$f"; printf '\n'
    fi
  done
  printf '\n'
done
} > "$tmp"

if [ "${1:-}" = "--check" ]; then
  cmp -s "$tmp" "$out" || { echo "$out is out of date: run packaging/gen-notices.sh" >&2; exit 1; }
  exit 0
fi
cp "$tmp" "$out"
echo "wrote $out ($(wc -l < "$out") lines)"
