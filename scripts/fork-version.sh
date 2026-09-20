#!/usr/bin/env sh
# Fork build version: <latest reachable vX.Y.Z tag>.<commits since that tag>.
#
# Upstream releases are plain vX.Y.Z. A fork build carries one extra numeric
# component so it orders ABOVE the release it is based on (updatecheck
# compares numeric dot-components, missing component = 0) while staying BELOW
# the next upstream release:
#
#   upstream v1.12.1 + 8 fork commits  ->  1.12.1.8
#   upstream v1.12.2 released          ->  1.12.2  (newer than 1.12.1.8)
#
# Stamp it into the binary with:
#   go build -ldflags "-s -w -X main.version=$(scripts/fork-version.sh)" ...
# (or `task build:fork`). Without the stamp the binary reports "dev".
set -eu

tag=$(git describe --tags --match 'v[0-9]*' --abbrev=0 HEAD 2>/dev/null || true)
if [ -z "$tag" ]; then
  # No version tag reachable (shallow clone): fall back to the module-less dev mark.
  echo dev
  exit 0
fi
rev=$(git rev-list --count "$tag"..HEAD)
printf '%s.%s\n' "${tag#v}" "$rev"
