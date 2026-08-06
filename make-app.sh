#!/bin/sh -e
# Bouwt hop-imager als macOS-app-bundle. Dat is niet cosmetica maar een EIS:
#
# macOS vraagt sinds Sequoia per app toestemming om met apparaten op het
# lokale netwerk te praten. Een losse binary (of een `go run`-build) heeft geen
# identiteit om die toestemming aan te hangen, en het gevolg is niet een
# dialoog maar STILTE: elke verbinding naar een LAN-peer faalt onmiddellijk met
# "no route to host". Gemeten 06-08 op Dereks Mac: dezelfde binary bereikt de
# router (192.168.1.1) en het internet prima — alleen de node op
# 192.168.1.193 was "onbereikbaar", terwijl ping en python er wél bij konden.
# De gateway en de eigen machine zijn namelijk vrijgesteld, andere LAN-hosts
# niet. FIND kan dus alleen werken vanuit een ondertekende bundle met een
# NSLocalNetworkUsageDescription.
#
#	./make-app.sh && open HopOS\ Imager.app
#
# Na de eerste scan vraagt macOS één keer toestemming; daarna staat de app in
# Systeeminstellingen → Privacy en beveiliging → Lokaal netwerk.
APP="HopOS Imager.app"
ID="org.gethop.imager"
VERSION="${VERSION:-0.0.0-dev}"

go build -trimpath -ldflags "-X main.version=$VERSION" -o hop-imager .

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS"
cp hop-imager "$APP/Contents/MacOS/hop-imager"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>          <string>hop-imager</string>
	<key>CFBundleIdentifier</key>          <string>$ID</string>
	<key>CFBundleName</key>                <string>HopOS Imager</string>
	<key>CFBundleDisplayName</key>         <string>HopOS Imager</string>
	<key>CFBundlePackageType</key>         <string>APPL</string>
	<key>CFBundleShortVersionString</key>  <string>$VERSION</string>
	<key>CFBundleVersion</key>             <string>$VERSION</string>
	<key>LSMinimumSystemVersion</key>      <string>12.0</string>
	<key>NSHighResolutionCapable</key>     <true/>
	<key>NSLocalNetworkUsageDescription</key>
	<string>HopOS Imager looks for running HopOS nodes on your network by connecting to their console port. It sends nothing but the connection itself — no keys, no credentials.</string>
</dict>
</plist>
PLIST

# Ad-hoc ondertekenen: TCC onthoudt de toestemming per bundle-identiteit, en
# zonder signatuur is die identiteit er niet — dan vraagt macOS het elke build
# opnieuw (of nooit).
codesign --force --sign - --identifier "$ID" "$APP" >/dev/null 2>&1 ||
	codesign --force --sign - "$APP"

echo "$APP klaar — start met: open '$APP'" >&2
echo "(eerste scan vraagt toestemming voor het lokale netwerk; daarna staat hij in Privacy → Lokaal netwerk)" >&2
