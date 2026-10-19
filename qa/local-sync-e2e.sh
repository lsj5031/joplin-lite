#!/usr/bin/env bash
# End-to-end test of the File system sync target (target 2) with two throwaway
# vaults that only share an on-disk Joplin target:
#
#   leg 1 (plaintext): vault A pushes a note, a fresh vault B pulls it back.
#   leg 2 (E2EE): an owner vault seeds a KeyV1 master key, pushes the note
#                 encrypted, a fresh vault D with the master password pulls and
#                 decrypts it, and a third vault without the password must not
#                 end up with a readable body.
#
#   bash qa/local-sync-e2e.sh
#
# Everything lands in tmp/local-sync-e2e. See docs/local-sync-test.md for the
# manual Joplin desktop interop test, which is the one gate this cannot cover.
set -euo pipefail
cd "$(dirname "$0")/.."

# Absolute paths go into the config files, in the form the Go binary can read:
# on Windows the shell's /t/... spelling would resolve against the current
# drive, so ask cygpath for the mixed T:/... form.
ROOT="$(pwd)"
if command -v cygpath >/dev/null 2>&1; then ROOT="$(cygpath -m "$ROOT")"; fi

RUN="tmp/local-sync-e2e"
STAMP="$(date +%Y%m%d-%H%M%S)"
TITLE="local round trip $STAMP"
BODY="body from the pushing vault $STAMP"
E2EE_TITLE="encrypted round trip $STAMP"
E2EE_BODY="secret body $STAMP"
MASTER_PASSWORD="local-e2e-master-$STAMP"
EXE=""
if [ "$(go env GOOS)" = "windows" ]; then EXE=".exe"; fi
BIN="tmp/gojoplin-e2e$EXE"
SEED="tmp/e2eeseed$EXE"
PIDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "vaults, targets and logs are in $RUN" >&2
  exit 1
}

rm -rf "$RUN"
mkdir -p "$RUN"
# The throwaway master password goes on disk so a failing run can be inspected
# (this is a test vault under tmp/, never a real one).
printf '%s\n' "$MASTER_PASSWORD" > "$RUN/master-password.txt"
go build -o "$BIN" .
go build -o "$SEED" ./tools/e2eeseed
echo "built $BIN and $SEED"

write_config() { # data-dir, target-dir, port, token, file
  cat > "$5" <<YAML
sync:
  target: 2
  local_dir: "$ROOT/$2"

api:
  token: "$4"
  key: "$4-key"
  master_password: "\${GOJOPLIN_MASTER_PASSWORD}"

# The clipper policy is read-only by default, which is right for third-party
# clients; this test vault is the owner of its data, so it may write anywhere.
mcp:
  allow_folders: "*"
  allow_tags: "*"
  allow_create_folder: true
  allow_create_tag: true

server:
  data_dir: "$ROOT/$1"
  port: $3
  listen_host: "127.0.0.1"

log_level: info
YAML
}

api() { # port, token, method, path, [json-body]
  local port="$1" token="$2" method="$3" path="$4" body="${5:-}"
  if [ -n "$body" ]; then
    curl -fsS -m 30 -X "$method" -H "Authorization: Bearer $token" \
      -H 'Content-Type: application/json' -d "$body" "http://127.0.0.1:$port$path"
  else
    curl -fsS -m 30 -X "$method" -H "Authorization: Bearer $token" "http://127.0.0.1:$port$path"
  fi
}

# start_serve runs the clipper API for a vault. The master password must be
# passed here too, not only to `sync`: a serve process syncs on its own, and a
# pull without the password stores the encrypted envelopes and serves those.
start_serve() { # config, port, token, log, [master-password]
  local pw="${5:-}"
  if [ -n "$pw" ]; then
    GOJOPLIN_MASTER_PASSWORD="$pw" "$BIN" serve --config "$1" >"$4" 2>&1 &
  else
    "$BIN" serve --config "$1" >"$4" 2>&1 &
  fi
  PIDS+=("$!")
  local i
  for i in $(seq 1 60); do
    if api "$2" "$3" GET /ping >/dev/null 2>&1; then return 0; fi
    sleep 0.25
  done
  return 1
}

stop_serve() {
  local last="${PIDS[${#PIDS[@]}-1]}"
  kill "$last" 2>/dev/null || true
  wait "$last" 2>/dev/null || true
  unset 'PIDS[${#PIDS[@]}-1]'
}

# create_note pushes a notebook and a note through the clipper API, so the items
# take the same service/ path the app uses, and echoes "<folder_id> <note_id>".
create_note() { # port, token, title, body
  local folder_json note_json folder_id note_id
  folder_json="$(api "$1" "$2" POST /folders "{\"title\":\"E2E $STAMP\"}")" ||
    fail "creating a notebook (port $1)"
  folder_id="$(printf '%s' "$folder_json" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)"
  [ -n "$folder_id" ] || fail "no notebook id in $folder_json"
  note_json="$(api "$1" "$2" POST /notes \
    "{\"title\":\"$3\",\"body\":\"$4\",\"parent_id\":\"$folder_id\"}")" ||
    fail "creating a note (port $1)"
  note_id="$(printf '%s' "$note_json" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)"
  [ -n "$note_id" ] || fail "no note id in $note_json"
  # The trailing newline matters: inside a process substitution a missing one
  # makes read hit EOF, which `set -e` treats as a failure.
  printf '%s %s\n' "$folder_id" "$note_id"
}

sync_vault() { # config, log, what
  "$BIN" sync --config "$1" >"$2" 2>&1 || {
    sed -n '$p' "$2" >&2
    fail "syncing $3; see $2"
  }
  grep -q "sync completed successfully" "$2" || fail "$3's sync did not report success; see $2"
}

# read_back checks a vault's API for the note, its body and (optionally) a
# resource blob. Both wait for the state to settle: a serve process syncs in the
# background from the moment it starts, so its first pull can still be writing
# the encrypted envelopes while the API already answers.
read_back() { # port, token, note_id, want_body, [resource_id]
  local i found=""
  for i in $(seq 1 60); do
    found="$(api "$1" "$2" GET "/notes/$3?fields=body" 2>/dev/null || true)"
    case "$found" in
      *"$4"*) break ;;
    esac
    sleep 0.25
  done
  case "$found" in
    *"$4"*) ;;
    *) fail "the note came back with a different body: ${found:-nothing}" ;;
  esac
  if [ -n "${5:-}" ]; then
    wait_resource "$1" "$2" "$5"
  fi
}

# wait_resource polls the blob endpoint until the decrypted bytes appear (or the
# deadline passes), for the same reason read_back retries the note: the resource
# blob can be re-fetched and re-decrypted by the vault's own background sync.
wait_resource() { # port, token, resource-id
  local out="$RUN/resource-read-$1.bin" i
  for i in $(seq 1 60); do
    if api "$1" "$2" GET "/resources/$3/file" >"$out" 2>/dev/null &&
      cmp -s "$out" <(printf 'encrypted resource blob\000\001\377'); then
      return 0
    fi
    sleep 0.25
  done
  fail "the resource blob did not survive the round trip (port $1): $(od -c "$out" | head -2)"
}

# ---------------------------------------------------------------- leg 1: plain

TARGET_A="$RUN/target-plain"
write_config "$RUN/vault-a" "$TARGET_A" 41841 "e2e-a-token" "$RUN/vault-a.yaml"
write_config "$RUN/vault-b" "$TARGET_A" 41842 "e2e-b-token" "$RUN/vault-b.yaml"

start_serve "$RUN/vault-a.yaml" 41841 "e2e-a-token" "$RUN/vault-a-serve.log" ||
  fail "vault A's API did not come up; see $RUN/vault-a-serve.log"
read -r folder_a note_a < <(create_note 41841 "e2e-a-token" "$TITLE" "$BODY")
echo "vault A created notebook $folder_a and note $note_a"
stop_serve
sync_vault "$RUN/vault-a.yaml" "$RUN/vault-a-sync.log" "vault A"

# The target itself must hold the note in the clear when no master password is
# set: this is what "plaintext" means on the wire.
[ -f "$TARGET_A/$note_a.md" ] || fail "$TARGET_A/$note_a.md is missing"
grep -q "$BODY" "$TARGET_A/$note_a.md" ||
  fail "the target's note file has no plaintext body; see $TARGET_A/$note_a.md"

sync_vault "$RUN/vault-b.yaml" "$RUN/vault-b-sync.log" "vault B"
start_serve "$RUN/vault-b.yaml" 41842 "e2e-b-token" "$RUN/vault-b-serve.log" ||
  fail "vault B's API did not come up; see $RUN/vault-b-serve.log"
read_back 41842 "e2e-b-token" "$note_a" "$BODY"
stop_serve
echo "leg 1 PASS: plaintext note round-tripped through the file-system target"

# ------------------------------------------------------------------ leg 2: E2EE

TARGET_B="$RUN/target-e2ee"
write_config "$RUN/vault-owner" "$TARGET_B" 41843 "e2e-owner-token" "$RUN/vault-owner.yaml"
write_config "$RUN/vault-e2ee" "$TARGET_B" 41844 "e2e-c-token" "$RUN/vault-e2ee.yaml"
write_config "$RUN/vault-locked" "$TARGET_B" 41845 "e2e-d-token" "$RUN/vault-locked.yaml"

# The owner owns the KeyV1 master key, the way Joplin desktop does after a user
# turns encryption on.
"$SEED" -data-dir "$RUN/vault-owner" -password "$MASTER_PASSWORD" \
  -title "$E2EE_TITLE" -body "$E2EE_BODY" >"$RUN/seed.log" ||
  fail "seeding the E2EE owner; see $RUN/seed.log"
note_c="$(grep '^note=' "$RUN/seed.log" | cut -d= -f2)"
res_c="$(grep '^resource=' "$RUN/seed.log" | cut -d= -f2)"
[ -n "$note_c" ] && [ -n "$res_c" ] || fail "the seeder printed no ids: $(cat "$RUN/seed.log")"
echo "owner seeded note $note_c and resource $res_c"

GOJOPLIN_MASTER_PASSWORD="$MASTER_PASSWORD" sync_vault "$RUN/vault-owner.yaml" \
  "$RUN/vault-owner-sync.log" "the E2EE owner"

# On the target everything must be an encrypted envelope: no plaintext title or
# body in the item, and an encrypted resource blob.
[ -f "$TARGET_B/$note_c.md" ] || fail "$TARGET_B/$note_c.md is missing"
grep -q "encryption_applied: 1" "$TARGET_B/$note_c.md" ||
  fail "the target's note is not marked encrypted; see $TARGET_B/$note_c.md"
if grep -q "$E2EE_BODY" "$TARGET_B/$note_c.md" || grep -q "$E2EE_TITLE" "$TARGET_B/$note_c.md"; then
  fail "plaintext leaked into the target's note file; see $TARGET_B/$note_c.md"
fi
grep -q '"e2ee"' "$TARGET_B/info.json" || fail "info.json has no e2ee section"
[ -f "$TARGET_B/.resource/$res_c" ] || fail "$TARGET_B/.resource/$res_c is missing"
if grep -aq "encrypted resource blob" "$TARGET_B/.resource/$res_c"; then
  fail "the resource blob is not encrypted on the target"
fi
echo "target holds encrypted envelopes only"

# A second vault with the master password joins the library: pull, then decrypt.
GOJOPLIN_MASTER_PASSWORD="$MASTER_PASSWORD" sync_vault "$RUN/vault-e2ee.yaml" \
  "$RUN/vault-e2ee-sync.log" "the E2EE joiner"
start_serve "$RUN/vault-e2ee.yaml" 41844 "e2e-c-token" "$RUN/vault-e2ee-serve.log" "$MASTER_PASSWORD" ||
  fail "the E2EE vault's API did not come up; see $RUN/vault-e2ee-serve.log"
read_back 41844 "e2e-c-token" "$note_c" "$E2EE_BODY" "$res_c"
stop_serve
echo "leg 2 PASS: encrypted note and resource round-tripped and decrypted"

# A locked vault may pull envelopes, but must fail closed before any upload.
# Preserve the target as an independent check; transient locks are excluded.
cp -R "$TARGET_B" "$RUN/target-before-locked"
if "$BIN" sync --config "$RUN/vault-locked.yaml" >"$RUN/vault-locked-sync.log" 2>&1; then
  fail "the locked vault unexpectedly reported a successful sync"
fi
grep -q "unlock the active encryption key before uploading" "$RUN/vault-locked-sync.log" ||
  fail "the locked vault failed for an unexpected reason; see $RUN/vault-locked-sync.log"
diff -qr -x locks "$RUN/target-before-locked" "$TARGET_B" ||
  fail "the locked vault changed the encrypted target"
# Nothing readable may land in the locked local store either.
start_serve "$RUN/vault-locked.yaml" 41845 "e2e-d-token" "$RUN/vault-locked-serve.log" ||
  fail "the locked vault's API did not come up; see $RUN/vault-locked-serve.log"
locked="$(api 41845 "e2e-d-token" GET "/notes/$note_c?fields=body" 2>/dev/null || true)"
stop_serve
case "$locked" in
  *"$E2EE_BODY"*) fail "a vault without the master password read the body: $locked" ;;
esac
if [ -z "$locked" ]; then
  echo "locked vault: the encrypted note is not exposed without the master password"
else
  echo "locked vault: the note exists but stays encrypted ($locked)"
fi

echo
echo "PASS: local file-system sync round-tripped plaintext and E2EE"
echo "  leg 1 note $note_a (plaintext target $TARGET_A)"
echo "  leg 2 note $note_c, resource $res_c (E2EE target $TARGET_B)"
echo "  vaults and logs: $RUN"
echo "Delete $RUN when you are done."
