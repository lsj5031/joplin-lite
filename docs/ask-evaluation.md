# Ask retrieval and answer evaluation

Last run: 2026-11-13. The corpus is intentionally small and fixed so changes to
retrieval, prompt construction, or citation handling are comparable across runs.
It is a regression check, not a benchmark or a claim about arbitrary vaults.

## Deterministic retrieval

Command:

```sh
CGO_ENABLED=0 go test ./internal/assistant -run TestAskRetrievalEvaluation -count=1 -v
```

The test seeds five notes and queries the real scoped BM25 path directly:

| Metric | Result |
| --- | ---: |
| Correct source in top 5 | 5/5 |
| Correct source at top 1 | 5/5 |
| Citation validator accepts only read evidence | 1/1 |
| Absent-note query returns no source | 1/1 |

The absent-note query (`passport renewal status`) returns zero retrieval results.
Unknown citation tokens (`[E99]`) never become verified citations.

## Live Splash answer path

Command:

```sh
ASK_LIVE_BASE_URL=http://127.0.0.1:11236/v1 \
ASK_LIVE_MODEL=main \
CGO_ENABLED=0 go test ./internal/assistant \
  -run TestSplashEvaluation -count=1 -v -timeout 8m
```

| Question | Correct source | Citation valid | Refused |
| --- | :---: | :---: | :---: |
| Camera repair follow-up | yes | yes | no |
| Tax filing deadline | yes | yes | no |
| Sourdough feed/bake schedule | yes | yes | no |
| Flight and hotel details | yes | yes | no |
| Passport renewal status | n/a | no citations | yes |

Observed totals: **4/4 correct sources**, **5/5 valid citation sets**, and
**1/1 refusals** on the absent-note question. The live test intentionally gates
source correctness, citation validity, and refusal behavior, but remains opt-in
because it requires a local model endpoint.

## Limitations

- Five notes and five live questions are enough to catch wiring regressions, not
  to estimate retrieval quality on a real vault.
- The corpus is lexical and English-only. It does not evaluate embeddings,
  multilingual notes, attachments, encrypted notes, or noisy OCR.
- The live run used one local model (`main`) at one point in time. It is not a
  model comparison.
- The live refusal check recognizes common refusal language; a paraphrased
  refusal could require a human review.
