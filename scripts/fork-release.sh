#!/usr/bin/env sh
# Publish a fork release that `-update` can see.
#
# Upstream release.yml publishes via GoReleaser, which hardcodes
# `release.github.owner: trefeon` in .goreleaser.yml and rejects a 4-component
# tag (X.Y.Z.<fork-rev> is not valid semver). Fork releases therefore go
# through this script: build the platform assets, write checksums.txt, and
# create the GitHub release on OUR repo with `gh`.
#
# Usage:  sh scripts/fork-release.sh [--dry-run]
# Requires: gh authenticated for the fork repo, plus go + tar + sha256sum.
set -eu

REPO=${FORK_RELEASES_REPO:-jhopan/freebuff-proxy}
DRY=0
[ "${1:-}" = "--dry-run" ] && DRY=1

cd "$(git rev-parse --show-toplevel)"
ver=$(sh scripts/fork-version.sh)
tag="v$ver"
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

echo "fork release: $tag (repo $REPO)"

# The updater matches an asset whose name ends in <goos>_<goarch>.tar.gz
# (zip on Windows) and an asset named exactly checksums.txt, so keep both
# names byte-identical to GoReleaser's: <project>_<version>_<os>_<arch>.<ext>.
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  ext=tar.gz
  [ "$os" = windows ] && ext=zip
  bin=freebuff-proxy
  [ "$os" = windows ] && bin=freebuff-proxy.exe
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -tags dashboard -trimpath -ldflags "-s -w -X main.version=$ver -X freebuff-proxy/backend/internal/cli/update.defaultReleasesRepo=$REPO" \
    -o "$out/$bin" ./backend/cmd/freebuff-proxy
  if [ "$ext" = zip ]; then
    if command -v zip >/dev/null 2>&1; then
      ( cd "$out" && zip -q "freebuff-proxy_${ver}_${os}_${arch}.zip" "$bin" )
    else
      # git-bash on Windows often lacks `zip`; python3 is the portable fallback.
      python -c "import sys,zipfile;z=zipfile.ZipFile(sys.argv[1],'w',zipfile.ZIP_DEFLATED);z.write(sys.argv[2],sys.argv[3]);z.close()" \
        "$out/freebuff-proxy_${ver}_${os}_${arch}.zip" "$out/$bin" "$bin"
    fi
  else
    tar -czf "$out/freebuff-proxy_${ver}_${os}_${arch}.tar.gz" -C "$out" "$bin"
  fi
  rm -f "$out/$bin"
done

( cd "$out" && sha256sum freebuff-proxy_* > checksums.txt )
ls -l "$out"

if [ "$DRY" -eq 1 ]; then
  echo "--dry-run: assets built, release not created"
  exit 0
fi

gh release create "$tag" --repo "$REPO" --title "$tag" \
  --notes "Fork release $tag (upstream base $(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo unknown), fork fixes on top)." \
  "$out"/freebuff-proxy_* "$out"/checksums.txt
echo "released: https://github.com/$REPO/releases/tag/$tag"
