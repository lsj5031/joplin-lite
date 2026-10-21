# Reproducible checks

The application tests live next to the Go source and in `web/*.test.mjs`.
This directory keeps disposable interoperability scenarios and the scripts
needed to reproduce the optional browser QA and product film.

From the repository root:

```sh
go test ./...
go test -tags s3 ./...
go -C shell test ./...
bash qa/local-sync-e2e.sh
bash qa/joplin-server-e2e.sh
bash qa/s3-e2e.sh
```

Read the corresponding guides in `docs/` before running a scenario. Scenario
profiles and targets belong under `qa/run/`; they are not application profiles.

The browser harness uses `qa/features.csv` as its machine-readable feature
specification. `qa/reset.sh`, the seed scripts and `qa/test_*.py` /
`qa/test_*.mjs` run against disposable backends. Results, screenshots and
activity logs are generated outputs and are deliberately not versioned.

The deterministic native product film is documented in `qa/promo/README.md`.
