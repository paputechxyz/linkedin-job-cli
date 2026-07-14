# Build the linkedin-jobs binary into the project root.
# Bumps VERSION automatically when Go source files change, and mirrors it
# into hermes-skill/SKILL.md so the CLI and skill versions never drift.
build:
    #!/usr/bin/env bash
    set -euo pipefail

    # Hash every Go source + module file. VERSION itself is deliberately
    # excluded so that bumping it doesn't itself trigger another bump on
    # the next build (which would loop).
    HASH=$(find . -type f \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) \
           -not -path './.git/*' \
           | sort \
           | xargs shasum \
           | shasum \
           | awk '{print $1}')

    VERSION="$(cat VERSION)"

    if [ ! -f .build-hash ]; then
        # First build on this machine — seed the hash, leave VERSION alone.
        echo "$HASH" > .build-hash
        echo "-> seeded build hash (version stays $VERSION)"
    elif [ "$(cat .build-hash)" = "$HASH" ]; then
        echo "-> no source changes (version stays $VERSION)"
    else
        # Source changed since last build — bump the patch component.
        NEW_VERSION=$(awk -F. -v OFS=. '{ $NF++; print }' VERSION)
        echo "$NEW_VERSION" > VERSION
        echo "$HASH"       > .build-hash
        VERSION="$NEW_VERSION"
        echo "-> source changed, bumped VERSION -> $VERSION"
    fi

    # Mirror VERSION into hermes-skill/SKILL.md so the skill frontmatter and
    # the CLI never drift apart. Idempotent — only rewrites when the value
    # differs, so it's a no-op once they're in sync. SKILL.md is excluded
    # from the source hash above, so this cannot cause a build loop.
    SKILL_MD="hermes-skill/SKILL.md"
    if [ -f "$SKILL_MD" ]; then
        CURRENT=$(awk '/^version: /{sub(/^version:[ \t]*/,""); print; exit}' "$SKILL_MD")
        if [ "$CURRENT" != "$VERSION" ]; then
            awk -v v="$VERSION" 'BEGIN{d=0} /^version: / && !d {$0="version: "v; d=1} {print}' \
                "$SKILL_MD" > "$SKILL_MD.tmp" && mv "$SKILL_MD.tmp" "$SKILL_MD"
            echo "-> synced SKILL.md version -> $VERSION"
        fi
    fi

    LDFLAGS="-X linkedin-jobs/cmd.Version=$VERSION"
    go build -ldflags "$LDFLAGS" -o linkedin-jobs .
    go install -ldflags "$LDFLAGS" .

# Release the current VERSION as a GitHub release with cross-compiled binaries.
#
# Flow: bump/sync VERSION via `just build` → refuse on re-tag or dirty tree
# (except the build-managed files) → cross-compile static binaries → commit the
# release metadata → git tag + push → `gh release create` with all assets.
#
# Requires: `gh` authenticated with push access to paputechxyz/linkedin-job-cli.
release:
    #!/usr/bin/env bash
    set -euo pipefail

    REPO="paputechxyz/linkedin-job-cli"

    # Ensure VERSION is current and the skill version mirrors it.
    just build >/dev/null

    VERSION="$(cat VERSION)"
    TAG="v${VERSION}"
    echo "-> releasing $TAG"

    # Refuse to re-release an existing tag.
    if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
        echo "-> tag ${TAG} already exists. Make source changes (which bump" >&2
        echo "   VERSION via just build) before releasing." >&2
        exit 1
    fi

    # Refuse if there is uncommitted work OUTSIDE the build-managed files.
    # VERSION + hermes-skill/SKILL.md are committed below as part of the release;
    # any other dirty file means the released binary would not match its tag.
    OTHER_DIRTY=""
    while IFS= read -r line; do
        path="${line:3}"
        case "$path" in
            VERSION|hermes-skill/SKILL.md) ;;
            *) OTHER_DIRTY="${OTHER_DIRTY}${line}"$'\n' ;;
        esac
    done < <(git status --porcelain --untracked-files=no)
    if [ -n "$OTHER_DIRTY" ]; then
        echo "-> working tree has uncommitted changes outside VERSION/SKILL.md:" >&2
        printf '%s' "$OTHER_DIRTY" | sed 's/^/    /' >&2
        echo "   commit them first, then run 'just release'." >&2
        exit 1
    fi

    # Cross-compile static binaries. The SQLite driver is pure Go
    # (modernc.org/sqlite), so CGO can be disabled for portable binaries.
    export CGO_ENABLED=0
    rm -rf dist && mkdir -p dist

    build_one() {
        local goos="$1" goarch="$2" ext=""
        [ "$goos" = "windows" ] && ext=".exe"
        local out="dist/linkedin-jobs_${goos}_${goarch}${ext}"
        printf '  -> %s\n' "$out"
        GOOS="$goos" GOARCH="$goarch" go build \
            -trimpath \
            -ldflags "-X linkedin-jobs/cmd.Version=${VERSION}" \
            -o "$out" .
    }

    build_one darwin arm64
    build_one darwin amd64
    build_one linux   amd64
    build_one linux   arm64
    build_one windows amd64

    # Commit the release metadata (if the build touched it), then tag + push.
    git add VERSION hermes-skill/SKILL.md
    if ! git diff --cached --quiet; then
        git commit -m "release ${TAG}"
    fi
    git tag "${TAG}"
    git push origin HEAD "${TAG}"

    # Publish the GitHub release with all binary assets.
    gh release create "${TAG}" \
        --repo "${REPO}" \
        --title "${TAG}" \
        --generate-notes \
        dist/linkedin-jobs_*

    echo "-> released ${TAG}: https://github.com/${REPO}/releases/tag/${TAG}"

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

serve:
    linkedin-jobs serve

rescore-all:
    linkedin-jobs rescore-all

rec:
    linkedin-jobs recommended --remote --hybrid --top 25 --min-salary 200000 --salary-currency CAD

url target_url:
    linkedin-jobs url '{{target_url}}' --remote --hybrid --top 25 --min-salary 200000 --salary-currency CAD

score-job job_id:
    linkedin-jobs job {{job_id}}

hr target_url:
    linkedin-jobs hr '{{target_url}}'

# Install the Hermes skill (symlinks ~/.hermes/skills/productivity/linkedin-jobs -> ./hermes-skill).
install-skill:
    #!/usr/bin/env bash
    set -euo pipefail
    TARGET="$HOME/.hermes/skills/productivity/linkedin-jobs"
    SOURCE="$(pwd)/hermes-skill"
    mkdir -p "$(dirname "$TARGET")"
    if [ -L "$TARGET" ]; then
        CURRENT="$(readlink "$TARGET")"
        if [ "$CURRENT" = "$SOURCE" ]; then
            echo "-> skill already installed at $TARGET"
            exit 0
        fi
        echo "-> replacing stale symlink at $TARGET"
        rm "$TARGET"
    elif [ -e "$TARGET" ]; then
        echo "-> $TARGET exists and is not a symlink; refusing to overwrite" >&2
        exit 1
    fi
    ln -s "$SOURCE" "$TARGET"
    echo "-> skill installed: $TARGET -> $SOURCE"
    echo "   Start a new Hermes session to discover the skill."

# Remove the Hermes skill symlink.
uninstall-skill:
    #!/usr/bin/env bash
    set -euo pipefail
    TARGET="$HOME/.hermes/skills/productivity/linkedin-jobs"
    if [ -L "$TARGET" ]; then
        rm "$TARGET"
        echo "-> skill removed: $TARGET"
    elif [ -e "$TARGET" ]; then
        echo "-> $TARGET exists and is not a symlink; refusing to remove" >&2
        exit 1
    else
        echo "-> nothing to remove at $TARGET"
    fi
