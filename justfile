# Build the linkedin-jobs binary into the project root and install it to $GOBIN.
#
# Versioning is owned by the CI release pipeline: release-please maintains a
# release PR from Conventional Commits and tags each release; GoReleaser injects
# the tag version into cmd.Version at build time (see .goreleaser.yml and
# .github/workflows/release.yml). Local builds therefore report version "dev".
build:
    go build -o linkedin-jobs .
    go install .

# Remove the linkedin-jobs binary from $GOBIN (or $GOPATH/bin).
uninstall:
    #!/usr/bin/env bash
    set -euo pipefail
    BIN="${GOBIN:-$(go env GOPATH)/bin}/linkedin-jobs"
    if [ -e "$BIN" ]; then
        rm "$BIN"
        echo "-> removed $BIN"
    else
        echo "-> nothing to remove at $BIN"
    fi
