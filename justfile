# Build the linkedin-jobs binary into the project root.
build:
    go build -o linkedin-jobs .
    go install .

serve:
    linkedin-jobs serve

score-all:
    linkedin-jobs score --all --local

rec:
    linkedin-jobs recommended --remote --top 10 --min-salary 200000 --salary-currency CAD