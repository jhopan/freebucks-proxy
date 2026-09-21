#!/usr/bin/env sh
# Publish a fork release that `-update` can see.
#
# Upstream release.yml publishes via GoReleaser, which hardcodes
# `release.github.owner: trefeon` in .goreleaser.yml and rejects a 4-component
# tag (X.Y.Z.<fork-rev> is not valid semver). Fork releases therefore go
# through this script: build the platform assets, write checksums.txt, and
# create the GitHub release on OUR repo with `gh`.
#
# NOTE: GitHub Actions is the canonical build path now
# (.github/workflows/fork-release.yml builds and publishes on a v* tag
# push). Keep this script as the offline fallback for when Actions is
# unavailable; it produces the identical asset names and layout.
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
  bin=freebucks-proxy
  [ "$os" = windows ] && bin=freebucks-proxy.exe
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -tags dashboard -trimpath -ldflags "-s -w -X main.version=$ver -X freebucks-proxy/backend/internal/cli/update.defaultReleasesRepo=$REPO" \
    -o "$out/$bin" ./backend/cmd/freebucks-proxy
  # Archive with an EXPLICIT 0755 mode: git-bash/MSYS tar on Windows writes
  # 0644 entries, so a Linux user extracting the asset gets a non-executable
  # binary (and `tar xzf && ./freebuff-proxy` fails). python's tarfile/zipfile
  # set the mode deterministically on every host.
  python - "$out" "$bin" "$ver" "$os" "$arch" "$ext" <<'PY'
import os, sys, tarfile, zipfile
out, bin_name, ver, goos, arch, ext = sys.argv[1:7]
src = os.path.join(out, bin_name)
base = "freebucks-proxy_%s_%s_%s" % (ver, goos, arch)
if ext == "zip":
    with zipfile.ZipFile(os.path.join(out, base + ".zip"), "w", zipfile.ZIP_DEFLATED) as z:
        zi = zipfile.ZipInfo(bin_name)
        zi.external_attr = 0o755 << 16  # unix mode in the zip external attrs
        z.writestr(zi, open(src, "rb").read())
else:
    with tarfile.open(os.path.join(out, base + ".tar.gz"), "w:gz") as t:
        ti = t.gettarinfo(src, arcname=bin_name)
        ti.mode = 0o755
        ti.uid = ti.gid = 0
        ti.uname = ti.gname = "root"
        with open(src, "rb") as fh:
            t.addfile(ti, fh)
PY
  rm -f "$out/$bin"
done

( cd "$out" && sha256sum freebucks-proxy_* > checksums.txt )
ls -l "$out"

if [ "$DRY" -eq 1 ]; then
  echo "--dry-run: assets built, release not created"
  exit 0
fi

gh release create "$tag" --repo "$REPO" --title "$tag" \
  --notes "Fork release $tag (upstream base $(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo unknown), fork fixes on top)." \
  "$out"/freebucks-proxy_* "$out"/checksums.txt
echo "released: https://github.com/$REPO/releases/tag/$tag"
