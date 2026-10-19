#!/usr/bin/env bash
# Печатает PDF-фикстуры из HTML через headless Chrome (см. README.md).
# Результат коммитится: тестам и CI Chrome не нужен.
set -euo pipefail
cd "$(dirname "$0")"
CHROME=${CHROME:-google-chrome}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

print() { # print <html> <pdf> [флаги]
  local src=$1 out=$2
  shift 2
  "$CHROME" --headless=new --no-sandbox --disable-gpu --no-pdf-header-footer "$@" \
    --print-to-pdf="$PWD/$out" "file://$PWD/$src" 2>/dev/null
}

print book.html outline.pdf --generate-pdf-document-outline
print book.html plain.pdf

# «Скан»: страница из одной картинки с текстом.
"$CHROME" --headless=new --no-sandbox --disable-gpu --hide-scrollbars --window-size=760,220 \
  --screenshot="$work/scan.png" "file://$PWD/scan-source.html" 2>/dev/null
cat > "$work/scanned.html" <<HTML
<!doctype html><html><head><meta charset="utf-8"><style>@page{size:A5;margin:1cm}body{margin:0}img{width:100%}</style></head>
<body><img src="scan.png" alt=""></body></html>
HTML
"$CHROME" --headless=new --no-sandbox --disable-gpu --no-pdf-header-footer \
  --print-to-pdf="$PWD/scanned.pdf" "file://$work/scanned.html" 2>/dev/null

ls -l ./*.pdf
