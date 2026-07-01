# Build the linkedin-jobs binary into the project root.
build:
    go build -o linkedin-jobs .
    go install .

serve:
    linkedin-jobs serve

score-all:
    linkedin-jobs score --all --local

rec:
    linkedin-jobs recommended --remote --hybrid --top 10 --min-salary 200000 --salary-currency CAD

url target_url:
    linkedin-jobs url '{{target_url}}' --remote --hybrid --top 10 --min-salary 200000