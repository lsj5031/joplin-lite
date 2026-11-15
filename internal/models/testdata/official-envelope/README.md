# Independent encrypted-envelope evidence

`items.json` was generated offline on 2026-11-15 from the cached official
Joplin Desktop **3.7.22**, build revision **95cab5a**, arm64 DMG. It is not an
export from a user profile. All item IDs, content and ciphertext are synthetic.

Provenance:

- DMG SHA-256: `185395eba8d9dac2d8656884cbb0e5b96bcb40b41d80c67126ce5526cb5186c8`
- `app.asar` SHA-256: `bfa30dd744852e73f69b96d5288db8c75429fdfb68bc4952f8d5de756fc1bbc0`
- `main-html.bundle.js` SHA-256:
  `b2143adff1589d07bd5c1dd83606ecbe571b61e5e6e27fa94357727e83675548`
- Shipped `packageInfo.js` identifies version/revision; the shipped source map
  maps the code to `../lib/models/BaseItem.ts`, `../lib/BaseModel.ts`,
  `../lib/JoplinDatabase.ts` and the database migration modules.

`qa/official-envelope.mjs` extracts and executes the shipped
`serializeForSync`, `serialize`, `serialize_format`, `unserialize` and
`unserialize_format` methods, plus the shipped database migration functions.
It has no dependency on Lite's serializers, parser, schema or repair code.
Only the five item tables are migrated in disposable in-memory SQLite.
Column order/types/defaults come from SQLite's resulting `PRAGMA table_info`,
not Lite's models. This includes otherwise-unmodeled resource OCR fields.

The harness adapts the field registry, scalar/date libraries and encryption
service boundary. Encryption returns a synthetic fixed string; it is not an
encryption interoperability test. The date adapter supports the ISO/epoch
values used here. Production ciphertext preservation and resource encryption
round trips are separately covered by Go tests.

For each type, it records actual official producer bytes and parser values
after the Synchronizer timestamp fallback. A schema-backed SQLite insert and
update succeed. Removing blank placeholders, parsing again and applying the
same fallback fails an existing-item SQLite update with
`NOT NULL constraint failed: <table>.user_created_time`. This is the failing
update-local path, not new-item save logic that may assign fresh timestamps.
SQL bindings map JavaScript `undefined` to NULL at the database boundary.
The harness does not execute the entire Synchronizer or BaseModel.save.

Regenerate using an extracted copy of this exact shipped bundle and Node with
`node:sqlite` (generated with Node 26.12.1):

```sh
node qa/official-envelope.mjs /absolute/path/main-html.bundle.js \
  /absolute/path/joplin-lite/internal/models/testdata/official-envelope/items.json
CGO_ENABLED=0 go test ./internal/models ./internal/e2ee ./internal/sync ./internal/enveloperepair ./cmd
```

The Go regression compares Lite's output byte-for-byte with these independent
producer bytes, checks conversion against the official parsed values, and
exercises the malformed and corrected update semantics in disposable SQLite.
Historical `../golden/envelope_*` passthrough fixtures are not interop evidence.

## Limits

The reported client is iOS **13.7.6**, release-3.7 revision **545a448ca**.
That exact source revision was not available in this airgapped environment.
The independently inspected producer/parser/schema is the cached official
3.7.22 build above, not an assertion of exact-revision or real-iOS testing.
The observed full-schema serialization and timestamp fallback agree with the
reported source evidence. Additional newer schema fields are blank and older
official parsers remove unknown fields.

No real iOS, Dropbox, live sync, user profile or user vault was accessed.
