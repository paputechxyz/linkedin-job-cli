# Build the linkedin-jobs binary into the project root.
# Bumps VERSION automatically when Go source files change.
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

    LDFLAGS="-X linkedin-jobs/cmd.Version=$VERSION"
    go build -ldflags "$LDFLAGS" -o linkedin-jobs .
    go install -ldflags "$LDFLAGS" .

serve:
    linkedin-jobs serve

score-all:
    linkedin-jobs score --all --local

rec:
    linkedin-jobs recommended --remote --hybrid --top 25 --min-salary 200000 --salary-currency CAD

url target_url:
    linkedin-jobs url '{{target_url}}' --remote --hybrid --top 25 --min-salary 200000 --salary-currency CAD

score-job job_id:
    linkedin-jobs score-job {{job_id}}

hr target_url:
    linkedin-jobs hr '{{target_url}}'
