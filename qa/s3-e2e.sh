#!/usr/bin/env bash
# End-to-end test of the S3 sync target (target 8) against a real object store.
#
# RustFS (S3-compatible, Apache-2.0) runs in Docker for the duration of the
# run; the binary is built with -tags s3 so the real backend is linked in.
# Three legs:
#
#   leg 0: the default (untagged) build refuses S3 with the stub's message.
#   leg 1 (plaintext): vault A pushes a note, a fresh vault B pulls it back,
#                      and the bucket holds the item in the clear.
#   leg 2 (E2EE): an owner vault seeds a KeyV1 master key, the bucket holds
#                 only envelopes, a vault with the master password decrypts,
#                 and a vault without it never exposes the body.
#
#   bash qa/s3-e2e.sh
#
# Point it at an existing store instead of starting RustFS:
#
#   S3_URL=https://s3.example.com S3_BUCKET=my-bucket \
#   AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... bash qa/s3-e2e.sh
#
# Everything lands in tmp/s3-e2e. The aws CLI is used to create the bucket and
# to inspect objects; the app itself never shell out to it.
set -euo pipefail
cd "$(dirname "$0")/.."

# Absolute paths go into the config files in the form the Go binary reads: on
# Windows the shell's /c/... spelling would resolve against the current drive.
ROOT="$(pwd)"
if command -v cygpath >/dev/null 2>&1; then ROOT="$(cygpath -m "$ROOT")"; fi

RUN="tmp/s3-e2e"
STAMP="$(date +%Y%m%d-%H%M%S)"
TITLE="s3 round trip $STAMP"
BODY="body from the pushing vault $STAMP"
E2EE_TITLE="encrypted round trip $STAMP"
E2EE_BODY="secret body $STAMP"
MASTER_PASSWORD="s3-e2e-master-$STAMP"

CONTAINER="${S3_CONTAINER:-rustfs-e2e}"
S3_IMAGE="${S3_IMAGE:-rustfs/rustfs:latest}"
S3_PORT="${S3_PORT:-19000}"
S3_CONSOLE_PORT="${S3_CONSOLE_PORT:-19001}"
S3_URL="${S3_URL:-http://127.0.0.1:$S3_PORT}"
S3_REGION="${S3_REGION:-us-east-1}"
BUCKET="${S3_BUCKET:-joplin-e2e}"
ACCESS_KEY="${AWS_ACCESS_KEY_ID:-rustfsadmin}"
SECRET_KEY="${AWS_SECRET_ACCESS_KEY:-rustfsadmin}"
STARTED_CONTAINER=0

EXE=""
if [ "$(go env GOOS)" = "windows" ]; then EXE=".exe"; fi
BIN="tmp/gojoplin-s3$EXE"
STUB="tmp/gojoplin-s3-stub$EXE"
SEED="tmp/e2eeseed$EXE"
PIDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
  if [ "$STARTED_CONTAINER" = 1 ]; then
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "vaults and logs are in $RUN" >&2
  exit 1
}

s3() { # args passed to aws s3 / s3api
  AWS_ACCESS_KEY_ID="$ACCESS_KEY" AWS_SECRET_ACCESS_KEY="$SECRET_KEY" \
    AWS_DEFAULT_REGION="$S3_REGION" aws --endpoint-url "$S3_URL" "$@"
}

s3cat() { # key
  s3 s3 cp "s3://$BUCKET/$1" - 2>/dev/null
}

command -v aws >/dev/null 2>&1 ||
  fail "the aws CLI is required (used to create the bucket and inspect objects)"

rm -rf "$RUN"
mkdir -p "$RUN"
printf '%s\n' "$MASTER_PASSWORD" > "$RUN/master-password.txt"

# ------------------------------------------------------------ the object store

if ! s3 s3api head-bucket --bucket "$BUCKET" >/dev/null 2>&1; then
  if ! curl -fsS -m 3 "$S3_URL/minio/health/live" >/dev/null 2>&1 &&
    ! curl -fsS -m 3 "$S3_URL/health" >/dev/null 2>&1; then
    echo "starting $S3_IMAGE as $CONTAINER on $S3_URL"
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    docker run --name "$CONTAINER" -d \
      -p "$S3_PORT:9000" -p "$S3_CONSOLE_PORT:9001" \
      -e RUSTFS_ACCESS_KEY="$ACCESS_KEY" -e RUSTFS_SECRET_KEY="$SECRET_KEY" \
      -e RUSTFS_VOLUMES=/data \
      -v "$ROOT/$RUN/data:/data" "$S3_IMAGE" >/dev/null ||
      fail "cannot start $S3_IMAGE"
    STARTED_CONTAINER=1
  fi

  for _ in $(seq 1 60); do
    if s3 s3api list-buckets >/dev/null 2>&1; then break; fi
    sleep 0.5
  done
  s3 s3api list-buckets >/dev/null 2>&1 ||
    fail "the object store at $S3_URL did not become ready"

  s3 s3 mb "s3://$BUCKET" >/dev/null 2>&1 || fail "cannot create bucket $BUCKET"
  echo "created bucket $BUCKET on $S3_URL"
else
  echo "using existing bucket $BUCKET on $S3_URL"
  # A leftover from an earlier run would make the delta non-empty and could
  # hand vault B someone else's note, so start from an empty bucket.
  s3 s3 rm "s3://$BUCKET" --recursive --quiet >/dev/null 2>&1 || true
fi

# ---------------------------------------------------------------- the binaries

go build -tags s3 -o "$BIN" . || fail "cannot build with -tags s3"
go build -o "$STUB" . || fail "cannot build the default binary"
go build -o "$SEED" ./tools/e2eeseed
echo "built $BIN (-tags s3) and $STUB (default)"

write_config() { # data-dir, port, token, file
  cat > "$4" <<YAML
sync:
  target: 8
  s3:
    bucket: "$BUCKET"
    url: "$S3_URL"
    region: "$S3_REGION"
    force_path_style: true

api:
  token: "$3"
  key: "$3-key"
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
  port: $2
  listen_host: "127.0.0.1"

log_level: info
YAML
}

run_bin() { # binary, args...
  local bin="$1"; shift
  AWS_ACCESS_KEY_ID="$ACCESS_KEY" AWS_SECRET_ACCESS_KEY="$SECRET_KEY" \
    AWS_DEFAULT_REGION="$S3_REGION" "$bin" "$@"
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
    GOJOPLIN_MASTER_PASSWORD="$pw" run_bin "$BIN" serve --config "$1" >"$4" 2>&1 &
  else
    run_bin "$BIN" serve --config "$1" >"$4" 2>&1 &
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

# create_note pushes a notebook and a note through the clipper API and echoes
# "<folder_id> <note_id>".
create_note() { # port, token, title, body
  local folder_json note_json folder_id note_id
  folder_json="$(api "$1" "$2" POST /folders "{\"title\":\"S3 E2E $STAMP\"}")" ||
    fail "creating a notebook (port $1)"
  folder_id="$(printf '%s' "$folder_json" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)"
  [ -n "$folder_id" ] || fail "no notebook id in $folder_json"
  note_json="$(api "$1" "$2" POST /notes \
    "{\"title\":\"$3\",\"body\":\"$4\",\"parent_id\":\"$folder_id\"}")" ||
    fail "creating a note (port $1)"
  note_id="$(printf '%s' "$note_json" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)"
  [ -n "$note_id" ] || fail "no note id in $note_json"
  printf '%s %s\n' "$folder_id" "$note_id"
}

sync_vault() { # config, log, what, [master-password]
  local pw="${4:-}"
  if [ -n "$pw" ]; then
    GOJOPLIN_MASTER_PASSWORD="$pw" run_bin "$BIN" sync --config "$1" >"$2" 2>&1 || {
      sed -n '$p' "$2" >&2
      fail "syncing $3; see $2"
    }
  else
    run_bin "$BIN" sync --config "$1" >"$2" 2>&1 || {
      sed -n '$p' "$2" >&2
      fail "syncing $3; see $2"
    }
  fi
  grep -q "sync completed successfully" "$2" || fail "$3's sync did not report success; see $2"
}

# read_back polls a vault's API for the note body (the vault's own background
# sync can still be writing when the API already answers) and, optionally, for
# a decrypted resource blob.
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
  if [ -n "${5:-}" ]; then wait_resource "$1" "$2" "$5"; fi
}

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

# ------------------------------------------------------- leg 0: stub build

write_config "$RUN/vault-stub" 41850 "e2e-stub-token" "$RUN/vault-stub.yaml"
if run_bin "$STUB" sync --config "$RUN/vault-stub.yaml" >"$RUN/stub-sync.log" 2>&1; then
  fail "the default build synced to S3; the driver should be behind -tags s3"
fi
grep -q "S3 sync target is not compiled into this build" "$RUN/stub-sync.log" ||
  fail "the default build failed without the stub's message; see $RUN/stub-sync.log"
echo "leg 0 PASS: the default build refuses S3 with the stub's message"

# ---------------------------------------------------------------- leg 1: plain

write_config "$RUN/vault-a" 41851 "e2e-a-token" "$RUN/vault-a.yaml"
write_config "$RUN/vault-b" 41852 "e2e-b-token" "$RUN/vault-b.yaml"

start_serve "$RUN/vault-a.yaml" 41851 "e2e-a-token" "$RUN/vault-a-serve.log" ||
  fail "vault A's API did not come up; see $RUN/vault-a-serve.log"
read -r folder_a note_a < <(create_note 41851 "e2e-a-token" "$TITLE" "$BODY")
echo "vault A created notebook $folder_a and note $note_a"
stop_serve
sync_vault "$RUN/vault-a.yaml" "$RUN/vault-a-sync.log" "vault A"

# The bucket itself must hold the note in the clear when no master password is
# set: that is what "plaintext" means on the wire.
s3cat "$note_a.md" > "$RUN/bucket-note-a.md" || fail "$note_a.md is not in the bucket"
grep -q "$BODY" "$RUN/bucket-note-a.md" ||
  fail "the bucket's note has no plaintext body; see $RUN/bucket-note-a.md"
s3cat info.json > "$RUN/bucket-info.json" || fail "info.json is not in the bucket"

sync_vault "$RUN/vault-b.yaml" "$RUN/vault-b-sync.log" "vault B"
start_serve "$RUN/vault-b.yaml" 41852 "e2e-b-token" "$RUN/vault-b-serve.log" ||
  fail "vault B's API did not come up; see $RUN/vault-b-serve.log"
read_back 41852 "e2e-b-token" "$note_a" "$BODY"
stop_serve
echo "leg 1 PASS: plaintext note round-tripped through the S3 target"

# ------------------------------------------------------------------ leg 2: E2EE

write_config "$RUN/vault-owner" 41853 "e2e-owner-token" "$RUN/vault-owner.yaml"
write_config "$RUN/vault-e2ee" 41854 "e2e-c-token" "$RUN/vault-e2ee.yaml"
write_config "$RUN/vault-locked" 41855 "e2e-d-token" "$RUN/vault-locked.yaml"

# The owner owns the KeyV1 master key, the way Joplin desktop does after a user
# turns encryption on.
"$SEED" -data-dir "$RUN/vault-owner" -password "$MASTER_PASSWORD" \
  -title "$E2EE_TITLE" -body "$E2EE_BODY" >"$RUN/seed.log" ||
  fail "seeding the E2EE owner; see $RUN/seed.log"
note_c="$(grep '^note=' "$RUN/seed.log" | cut -d= -f2)"
res_c="$(grep '^resource=' "$RUN/seed.log" | cut -d= -f2)"
[ -n "$note_c" ] && [ -n "$res_c" ] || fail "the seeder printed no ids: $(cat "$RUN/seed.log")"
echo "owner seeded note $note_c and resource $res_c"

sync_vault "$RUN/vault-owner.yaml" "$RUN/vault-owner-sync.log" "the E2EE owner" "$MASTER_PASSWORD"

# In the bucket everything must be an encrypted envelope: no plaintext title or
# body in the item, an encrypted resource blob, and E2EE recorded in info.json.
s3cat "$note_c.md" > "$RUN/bucket-note-c.md" || fail "$note_c.md is not in the bucket"
grep -q "encryption_applied: 1" "$RUN/bucket-note-c.md" ||
  fail "the bucket's note is not marked encrypted; see $RUN/bucket-note-c.md"
if grep -q "$E2EE_BODY" "$RUN/bucket-note-c.md" || grep -q "$E2EE_TITLE" "$RUN/bucket-note-c.md"; then
  fail "plaintext leaked into the bucket's note; see $RUN/bucket-note-c.md"
fi
s3cat info.json > "$RUN/bucket-info-e2ee.json" || fail "info.json is not in the bucket"
grep -q '"e2ee"' "$RUN/bucket-info-e2ee.json" || fail "info.json has no e2ee section"
s3cat ".resource/$res_c" > "$RUN/bucket-resource-c.bin" ||
  fail ".resource/$res_c is not in the bucket"
if grep -aq "encrypted resource blob" "$RUN/bucket-resource-c.bin"; then
  fail "the resource blob is not encrypted in the bucket"
fi
echo "bucket holds encrypted envelopes only"

# A second vault with the master password joins the library: pull, then decrypt.
sync_vault "$RUN/vault-e2ee.yaml" "$RUN/vault-e2ee-sync.log" "the E2EE joiner" "$MASTER_PASSWORD"
start_serve "$RUN/vault-e2ee.yaml" 41854 "e2e-c-token" "$RUN/vault-e2ee-serve.log" "$MASTER_PASSWORD" ||
  fail "the E2EE vault's API did not come up; see $RUN/vault-e2ee-serve.log"
read_back 41854 "e2e-c-token" "$note_c" "$E2EE_BODY" "$res_c"
stop_serve
echo "leg 2 PASS: encrypted note and resource round-tripped and decrypted"

# Without the master password nothing readable may land in the local store.
sync_vault "$RUN/vault-locked.yaml" "$RUN/vault-locked-sync.log" "the locked vault"
start_serve "$RUN/vault-locked.yaml" 41855 "e2e-d-token" "$RUN/vault-locked-serve.log" ||
  fail "the locked vault's API did not come up; see $RUN/vault-locked-serve.log"
locked="$(api 41855 "e2e-d-token" GET "/notes/$note_c?fields=body" 2>/dev/null || true)"
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
echo "PASS: S3 sync round-tripped plaintext and E2EE against $(printf '%s' "$S3_URL")"
echo "  leg 1 note $note_a (plaintext bucket $BUCKET)"
echo "  leg 2 note $note_c, resource $res_c (E2EE bucket $BUCKET)"
echo "  vaults and logs: $RUN"
echo "Delete $RUN when you are done."
