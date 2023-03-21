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

mkdir tmpgit
cd tmpgit
git init
git config user.name cueckoo
git config user.email cueckoo@gmail.com
git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} | base64)"
git fetch https://review.gerrithub.io/a/cue-lang/cue "${{ github.event.client_payload.refs }}"
git checkout -b ${{ github.event.client_payload.targetBranch }} FETCH_HEAD

# Fail if we already have a trybot trailer
currTrailer="$(git log -1 --pretty='%(trailers:key=TryBot-Trailer,valueonly)')"
if [[ "$currTrailer" != "" ]]; then
	echo "Commit for refs ${{ github.event.client_payload.refs }} already has TryBot-Trailer"
	exit 1
fi

trailer="$(cat <<EOD | tr '\n' ' '
${{ toJSON(github.event.client_payload) }}
EOD
)"

git log -1 --format=%B | git interpret-trailers --trailer "TryBot-Trailer: $trailer" | git commit --amend -F -

for try in {1..20}; do
	echo $try
  	git push -f https://github.com/cue-lang/cue-trybot ${{ github.event.client_payload.targetBranch }}:${{ github.event.client_payload.targetBranch }} && break
  	sleep 1
done
ABCDEF