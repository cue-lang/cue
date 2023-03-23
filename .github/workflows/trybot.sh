set -euo pipefail

sub() {
	sed -e 's+${{ secrets.CUECKOO_GERRITHUB_PASSWORD }}+$CUECKOO_GERRITHUB_PASSWORD+g' |
		sed -e 's+${{ secrets.CUECKOO_GITHUB_PAT }}+$CUECKOO_GITHUB_PAT+g' |
		sed -e 's+${{ github.event.client_payload.refs }}+refs/changes/52/551352/$PATCHSET+g' |
		sed -e 's+${{ github.event.client_payload.targetBranch }}+master+g' |
		sed -e 's+${{ toJSON(github.event.client_payload) }}+{ "type": "trybot", "refs": "refs/changes/52/551352/$PATCHSET", "CL": 551352, "patchset": $PATCHSET, "targetBranch": "main" }+g'
}

cat <<"ABCDEF" | sub | bash
set -euxo pipefail
cd $(mktemp -d)

cat <<EOD > ~/.netrc
machine review.gerrithub.io
login cueckoo
password ${{ secrets.CUECKOO_GERRITHUB_PASSWORD }}
EOD
chmod 600 ~/.netrc

ref="$(echo ${{github.event.client_payload.payload.ref}} | sed -E 's/^refs\/changes\/[0-9]+\/([0-9]+)\/([0-9]+).*/\1\/\2/')"
echo "gerrithub_ref=$ref" >> $GITHUB_OUTPUT
ABCDEF