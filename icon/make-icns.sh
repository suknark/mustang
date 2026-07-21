#!/bin/sh
# Turns icon/icon-1024.png (produced by `go run ./icon <artwork.png>`) into
# icon/Mustang.icns with all required sizes.
set -e
cd "$(dirname "$0")"

rm -rf Mustang.iconset
mkdir Mustang.iconset
for s in 16 32 128 256 512; do
	sips -z "$s" "$s" icon-1024.png --out "Mustang.iconset/icon_${s}x${s}.png" >/dev/null
	sips -z "$((s * 2))" "$((s * 2))" icon-1024.png --out "Mustang.iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns Mustang.iconset -o Mustang.icns
rm -rf Mustang.iconset
echo "built icon/Mustang.icns"
