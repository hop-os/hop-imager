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

gh release upload "$TAG" --clobber "$ASSET"
gh release upload rolling-release --clobber "$ASSET"
echo ">> $ASSET genotariseerd en geüpload naar $TAG en rolling-release" >&2
