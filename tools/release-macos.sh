#!/bin/sh -e
# Bouwt de macOS-release zoals hij hoort: app-bundle met icoon, ondertekend met
# Developer ID, verpakt met ditto, en genotariseerd als daar inloggegevens voor
# zijn.
#
#	VERSION=1.1.1 tools/release-macos.sh
#	VERSION=1.1.1 NOTARY_PROFILE=hopos tools/release-macos.sh   (mét notarisatie)
#
# DRIE DINGEN DIE HIER NIET WILLEKEURIG ZIJN:
#
#  1. ONDERTEKENEN MET DEVELOPER ID, niet ad-hoc. macOS onthoudt toestemmingen
#     (zoals het lokale netwerk, dat FIND nodig heeft) per signing-identiteit.
#     Ad-hoc geeft elke build een nieuwe identiteit, dus vraagt het systeem het
#     elke keer opnieuw; met een vaste Developer ID is één keer toestaan genoeg
#     — ook na een update.
#  2. VERPAKKEN MET DITTO, niet met zip. `ditto -c -k --sequesterRsrc
#     --keepParent` is Apple's eigen weg en houdt symlinks, resource forks en
#     dus de signatuur intact; een gewone zip kan een bundle onderweg slopen en
#     dan faalt de verificatie pas bij de gebruiker.
#  3. NOTARISEREN als het kan. Zonder notarisatie weigert Gatekeeper een
#     gedownloade app óók als hij netjes met Developer ID ondertekend is
#     ("Unnotarized Developer ID"). Mét notarisatie plus stapling opent hij
#     zonder één waarschuwing, ook offline.
#
# Eenmalig inloggegevens opslaan (App Store Connect-key is de nettere weg, maar
# een app-specifiek wachtwoord werkt ook):
#
#	xcrun notarytool store-credentials hopos \
#	    --apple-id <apple-id> --team-id QVY3KR44QS --password <app-specifiek-wachtwoord>
cd "$(dirname "$0")/.."
VERSION="${VERSION:-0.0.0-dev}"
APP="HopOS Imager.app"
ASSET="hop-imager-macos-$(uname -m | sed 's/x86_64/amd64/').zip"

VERSION="$VERSION" ./make-app.sh

# Verificatie vóór het verpakken: een bundle die hier niet klopt, klopt bij de
# gebruiker ook niet.
codesign --verify --strict --verbose=1 "$APP"

if [ -n "$NOTARY_PROFILE" ]; then
	echo ">> notariseren via profiel $NOTARY_PROFILE (dit duurt een paar minuten)" >&2
	rm -f /tmp/notarize.zip
	ditto -c -k --sequesterRsrc --keepParent "$APP" /tmp/notarize.zip
	xcrun notarytool submit /tmp/notarize.zip --keychain-profile "$NOTARY_PROFILE" --wait
	# Stapling op de APP (een zip kun je niet stapelen), daarna opnieuw
	# verpakken — dan zit het ticket in wat de gebruiker downloadt en werkt het
	# ook zonder internet.
	xcrun stapler staple "$APP"
	xcrun stapler validate "$APP"
fi

rm -f "$ASSET"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$ASSET"

echo "" >&2
echo "$ASSET klaar ($(du -h "$ASSET" | cut -f1))" >&2
spctl -a -vvv "$APP" 2>&1 | sed 's/^/  gatekeeper: /' >&2 || true
