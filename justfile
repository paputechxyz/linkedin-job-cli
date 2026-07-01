# Build the linkedin-jobs binary into the project root.
build:
    go build -o linkedin-jobs .
    go install .

serve:
    linkedin-jobs serve

score-all:
    linkedin-jobs score --all
