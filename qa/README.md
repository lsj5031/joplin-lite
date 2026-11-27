# Reproducible checks

The application tests live next to the Go source. This directory keeps
disposable interoperability scenarios and the scripts that seed and drive them.

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

`qa/features.csv` is the machine-readable feature specification, and
`qa/collect_go.py` maps the Go suites onto it. The seed scripts and
`qa/test_*.py` run against disposable backends. Results, screenshots and
activity logs are generated outputs and are deliberately not versioned.

The UI is verified by the Go tests in `shell/`, not by a browser: the browser
preview and its Playwright scripts are gone. The product film is rendered from
native captures, as `qa/promo/README.md` describes.
