#!/bin/sh
# scripts/release.sh VERSION — the release, as one command.
#
# Four binaries, each carrying at link time the version it was built as, a
# checksums file, the plugin zipped with all four inside, and the repo's own
# marketplace pointed at the zip: what a stranger's `/plugin install
# drawer@drawer` downloads, sha256 checked, with no Go, no build and nothing
# on their machine. `drawer -version` then names what they have. The pins
# for the bare binaries go into scripts/drawer, so a checkout that arrived
# by git — an organisation pushing the plugin can only point at git —
# downloads the same binary the zip would have carried and checks it against
# the same sum.
#
# Order matters and is the reason this is a script: the binaries' sums go
# into the wrapper, the wrapper goes into the zip, the zip's sum goes into
# the marketplace, and the commit carries all three.
#
#   scripts/release.sh 0.2.0             # build, rewrite, commit, tag, push, release
#   DRAWER_RELEASE_DRY=1 scripts/release.sh 0.2.0   # build and rewrite; no git, no gh
#
# Needs: go, gh (logged in), and a clean tree on main. A dry run leaves the
# rewritten files in the tree for reading; `git checkout` takes them back.
set -e
cd "$(dirname "$0")/.."
v=${1:?usage: scripts/release.sh VERSION}
dry=${DRAWER_RELEASE_DRY:-}
repo=sureffi/drawer
url=https://github.com/$repo/releases/download/v$v

if [ -z "$dry" ]; then
	[ -z "$(git status --porcelain)" ] || { echo "release: the tree is not clean" >&2; exit 1; }
	[ "$(git branch --show-current)" = main ] || { echo "release: not on main" >&2; exit 1; }
	command -v gh >/dev/null 2>&1 || { echo "release: gh is not on the PATH" >&2; exit 1; }
fi
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
	else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

rm -rf dist
mkdir -p dist/plugin/scripts/bin

# The binaries: static, stripped, paths trimmed, the version linked in, one
# per platform the wrapper knows how to name.
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	os=${t%/*}
	arch=${t#*/}
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
		-ldflags="-s -w -X github.com/sureffi/drawer/internal/drawer.version=$v" \
		-o "dist/drawer-$os-$arch" ./cmd/drawer
	echo "built drawer-$os-$arch $(du -h "dist/drawer-$os-$arch" | cut -f1)"
done
# The version, out of the one binary this box can run. -X names a package
# path and a variable, and neither the linker nor the build says a word when
# the name stops matching: every download would then call itself dev and the
# first reader to ask which one they have gets the wrong answer.
host="dist/drawer-$(go env GOOS)-$(go env GOARCH)"
if [ -x "$host" ]; then
	[ "$("$host" -version)" = "drawer $v" ] ||
		{ echo "release: the version did not link in" >&2; exit 1; }
	echo "linked drawer $v"
else
	echo "release: no native build here to ask; the version is unchecked" >&2
fi

(cd dist && for f in drawer-*; do echo "$(sha256 "$f")  $f"; done >checksums.txt)

# The pins, into the wrapper.
sed -i.bak "s|^release=.*|release=$v|" scripts/drawer
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	os=${t%/*}
	arch=${t#*/}
	sed -i.bak "s|^sha_${os}_${arch}=.*|sha_${os}_${arch}=$(sha256 "dist/drawer-$os-$arch")|" scripts/drawer
done
rm -f scripts/drawer.bak
sed -i.bak "s|\"version\": \"[^\"]*\"|\"version\": \"$v\"|" .claude-plugin/plugin.json
rm -f .claude-plugin/plugin.json.bak

# The zip: the plugin and nothing else, binaries inside, at the zip's root.
cp -r hooks LICENSE NOTICE README.md dist/plugin/
mkdir -p dist/plugin/.claude-plugin
cp .claude-plugin/plugin.json dist/plugin/.claude-plugin/
cp scripts/drawer dist/plugin/scripts/
cp dist/drawer-* dist/plugin/scripts/bin/
if command -v zip >/dev/null 2>&1; then
	(cd dist/plugin && zip -q -r -9 ../drawer-plugin.zip .)
else
	(cd dist/plugin && python3 -c '
import os, zipfile
with zipfile.ZipFile("../drawer-plugin.zip", "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
    for d, _, fs in os.walk("."):
        for f in fs:
            p = os.path.join(d, f)
            z.write(p, os.path.relpath(p, "."))
')
fi
echo "zipped drawer-plugin.zip $(du -h dist/drawer-plugin.zip | cut -f1)"

# The marketplace: the repo's own, pointed at the zip.
cat >.claude-plugin/marketplace.json <<JSON
{
  "name": "drawer",
  "owner": { "name": "Sureffi", "email": "sureffi@proton.me" },
  "description": "drawer, from its own repository.",
  "plugins": [
    {
      "name": "drawer",
      "source": {
        "source": "archive",
        "url": "$url/drawer-plugin.zip",
        "sha256": "$(sha256 dist/drawer-plugin.zip)"
      },
      "version": "$v",
      "description": "A \`\`\`dot fence in a reply is drawn in place."
    }
  ]
}
JSON

if [ -n "$dry" ]; then
	echo "dry: dist/ built; scripts/drawer, plugin.json and marketplace.json rewritten; no git, no gh"
	exit 0
fi
# The commit and the tag, unless a run before this one already made them:
# a re-run after a failed upload picks up where it stopped.
if git rev-parse -q --verify "refs/tags/v$v" >/dev/null; then
	echo "tag v$v exists; uploading to it"
else
	git add .claude-plugin/plugin.json .claude-plugin/marketplace.json scripts/drawer
	git commit -q -m "drawer: release v$v"
	git tag "v$v"
	git push -q origin main "v$v"
fi
# The release, then its assets. Separately, and the upload retried: on a
# repository minutes old GitHub's upload host answered 404 to the first
# release ever made on it, and gh took the half-made release down with it.
gh release view "v$v" >/dev/null 2>&1 || gh release create "v$v" --title "drawer v$v" \
	--notes "\`/plugin marketplace add $repo\` then \`/plugin install drawer@drawer\`. The zip is the plugin with every binary inside; the bare binaries are what a git checkout downloads, checked against checksums.txt."
n=0
until gh release upload "v$v" dist/drawer-* dist/checksums.txt dist/drawer-plugin.zip --clobber; do
	n=$((n + 1))
	[ $n -lt 5 ] || { echo "release: the upload failed five times" >&2; exit 1; }
	echo "release: upload failed; again in 10s ($n)" >&2
	sleep 10
done
echo "released v$v: $(gh release view "v$v" --json url --jq .url)"
