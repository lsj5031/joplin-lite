# Golden corpus — the format gate

Each fixture is Joplin's canonical serialized item text and must round-trip
byte-for-byte. The gate is asserted in `golden_test.go`:

1. **raw round-trip** — `ParseItem(x).Bytes() == x` for every fixture.
2. **merge round-trip** — `ParseItem(x).SerializeWithOverrides(typedFields(x)) == x`
   for every fixture except the `envelope_*` forms (their metadata block is a
   reduced keep-keys set, so a full field map legitimately appends keys).
3. **typed round-trip** — `Serialize(Deserialize(x)) == x` for fixtures whose
   keys are all model-known (`*_extra` fixtures carry unknown keys such as
   `is_locked` and `extracted_resource_ids`, which survive only through the raw
   layer).

Fixture rules (they encode Joplin's writer, verified against
`packages/lib/models/BaseItem.ts`):

- No trailing newline (Joplin's writer emits none).
- Sections are `title`, `body` (omitted when empty), then metadata, separated
  by blank lines.
- Only `created_time`, `updated_time`, `user_created_time`, `user_updated_time`
  are ISO 8601; `order`, `todo_due`, `todo_completed`, `deleted_time` and
  `blob_updated_time` are plain millisecond numbers ("0" when zero).
- `latitude`/`longitude` are fixed 8 decimals, `altitude` fixed 4.
- Metadata values escape only `\n`/`\r` sequences and real newlines/CRs — a
  bare backslash is untouched (see `note_paths.md`).
- `type_` is last. Type ids: 1 note, 2 folder, 4 resource, 5 tag, 6 note-tag,
  9 master key.

Fixtures are synthetic but shaped exactly like real target items; the M1 gate
replaces/extends them with items exported from the real target.
