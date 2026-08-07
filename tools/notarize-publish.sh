#!/bin/sh -e
# Notariseert de macOS-release en zet hem online. Bedoeld om te kunnen WACHTEN:
# Apple's notary-service weigert met 403 zolang er een openstaande overeenkomst
# is, en dat kan minuten tot uren duren nadat je hem in de portal accepteert.
# Dus: eerst proberen inloggegevens op te slaan, en pas dóórgaan als dat lukt.
#
#	VERSION=1.1.1 TAG=v1.1.1 tools/notarize-publish.sh
#
# Verwacht .notary in de repo-root (gitignored, mode 600) met APPLE_ID, TEAM_ID,
# APP_PASSWORD en NOTARY_PROFILE.
cd "$(dirname "$0")/.."
. ./.notary

VERSION="${VERSION:?zet VERSION=x.y.z}"
TAG="${TAG:-v$VERSION}"
ASSET="hop-imager-macos-universal.zip"

if ! xcrun notarytool store-credentials "$NOTARY_PROFILE" \
	--apple-id "$APPLE_ID" --team-id "$TEAM_ID" --password "$APP_PASSWORD" >/dev/null 2>&1; then
	echo "notary weigert nog (openstaande overeenkomst bij Apple?) — niets gedaan" >&2
	exit 1
fi
echo ">> inloggegevens opgeslagen als profiel '$NOTARY_PROFILE'" >&2

VERSION="$VERSION" NOTARY_PROFILE="$NOTARY_PROFILE" tools/release-macos.sh

# Het bewijs dat het echt genotariseerd IS: Gatekeeper moet 'accepted' zeggen.
if ! spctl -a -vvv "HopOS Imager.app" 2>&1 | grep -q accepted; then
	echo "FOUT: Gatekeeper accepteert de app nog niet — niet uploaden" >&2
	spctl -a -vvv "HopOS Imager.app" >&2 2>&1 || true
	exit 1
fi

# De release kan er nog niet zijn: CI maakt hem, en die kan later klaar zijn dan
# de notarisatie hier (die duurt minuten). Dan maken we hem zelf aan in plaats
# van te falen op "release not found".
gh release view "$TAG" >/dev/null 2>&1 ||
	gh release create "$TAG" --title "hop-imager $TAG" --notes-file .github/release-notes.md
gh release upload "$TAG" --clobber "$ASSET"
gh release upload rolling-release --clobber "$ASSET"

# SHA256SUMS bijwerken. CI maakt dat bestand voor wat DAAR gebouwd is; de
# macOS-asset komt hiervandaan, dus zonder deze stap dekt de checksumlijst juist
# de grootste download niet — een half bewijs is geen bewijs.
for tag in "$TAG" rolling-release; do
	tmp="$(mktemp -d)"
	if gh release download "$tag" --pattern SHA256SUMS --dir "$tmp" 2>/dev/null; then
		grep -v " $ASSET\$" "$tmp/SHA256SUMS" > "$tmp/new" || true
	else
		: > "$tmp/new"
	fi
	shasum -a 256 "$ASSET" >> "$tmp/new"
	sort -k2 "$tmp/new" > "$tmp/SHA256SUMS"
	gh release upload "$tag" --clobber "$tmp/SHA256SUMS"
	rm -rf "$tmp"
done
echo ">> $ASSET genotariseerd, geüpload naar $TAG en rolling-release, SHA256SUMS bijgewerkt" >&2
