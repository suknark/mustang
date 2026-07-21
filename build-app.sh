#!/bin/sh
# Builds Mustang.app — a standalone bundle so macOS permissions (Microphone,
# Accessibility) are granted to the app itself, not to your terminal.
#
# Signing: a stable identity makes TCC grants survive rebuilds. The script
# uses $MUSTANG_SIGN_ID if set, otherwise the first "Apple Development"
# certificate in your keychain, otherwise an ad-hoc signature (works, but
# you will have to re-grant Accessibility after every rebuild).
set -e
cd "$(dirname "$0")"

BUNDLE_ID="${MUSTANG_BUNDLE_ID:-com.mustang.app}"

go build -o mustang .

APP=dist/Mustang.app
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp mustang "$APP/Contents/MacOS/mustang"

# Optional icon: generate one from any square artwork with `go run ./icon
# <art.png>` + `icon/make-icns.sh` (see icon/README section in README.md).
if [ -f icon/Mustang.icns ]; then
	cp icon/Mustang.icns "$APP/Contents/Resources/Mustang.icns"
fi

cat > "$APP/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>            <string>Mustang</string>
	<key>CFBundleDisplayName</key>     <string>Mustang</string>
	<key>CFBundleIdentifier</key>      <string>${BUNDLE_ID}</string>
	<key>CFBundleExecutable</key>      <string>mustang</string>
	<key>CFBundlePackageType</key>     <string>APPL</string>
	<key>CFBundleVersion</key>         <string>1.0</string>
	<key>CFBundleShortVersionString</key> <string>1.0</string>
	<key>LSUIElement</key>             <true/>
	<key>CFBundleIconFile</key>        <string>Mustang</string>
	<key>NSMicrophoneUsageDescription</key>
	<string>Mustang listens to the microphone to detect finger snaps.</string>
</dict>
</plist>
EOF

SIGN_ID="${MUSTANG_SIGN_ID:-$(security find-identity -v -p codesigning 2>/dev/null |
	awk -F'"' '/Apple Development/{print $2; exit}')}"
if [ -n "$SIGN_ID" ]; then
	echo "signing as: $SIGN_ID"
	codesign --force --sign "$SIGN_ID" "$APP"
else
	echo "warning: no Apple Development certificate found, signing ad-hoc" >&2
	echo "         (TCC grants will not survive rebuilds)" >&2
	codesign --force --sign - "$APP"
fi

echo "built $APP"
