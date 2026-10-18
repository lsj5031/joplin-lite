#!/usr/bin/env bash
# End-to-end test against a real Joplin Server (sync target 9): two throwaway
# vaults share one server, A pushes a note and a fresh B pulls it back. B never
# sees A's database, so a match proves the round trip through the server, not a
# local copy.
#
#   JOPLIN_SERVER_URL=https://sync.example.com \
#   GOJOPLIN_USERNAME=me@example.com GOJOPLIN_PASSWORD='...' \
#   bash qa/joplin-server-e2e.sh
#
# Set GOJOPLIN_MASTER_PASSWORD as well to test the same round trip with E2EE
# (items on the server are then encrypted envelopes, and B decrypts them).
#
# Everything it writes lands in tmp/joplin-server-e2e: two vaults, their
# configs and their logs. Nothing touches a real Joplin profile or library.
# See docs/joplin-server-test.md.
set -euo pipefail
cd "$(dirname "$0")/.."

URL="${JOPLIN_SERVER_URL:-}"
USERNAME="${GOJOPLIN_USERNAME:-}"
PASSWORD="${GOJOPLIN_PASSWORD:-}"
MASTER_PASSWORD="${GOJOPLIN_MASTER_PASSWORD:-}"

if [ -z "$URL" ] || [ -z "$USERNAME" ] || [ -z "$PASSWORD" ]; then
  cat >&2 <<'USAGE'
usage: JOPLIN_SERVER_URL=... GOJOPLIN_USERNAME=... GOJOPLIN_PASSWORD=... \
         bash qa/joplin-server-e2e.sh

  JOPLIN_SERVER_URL          base URL of a Joplin Server, e.g. http://127.0.0.1:22300
  GOJOPLIN_USERNAME          account email registered on that server
  GOJOPLIN_PASSWORD          its password
  GOJOPLIN_MASTER_PASSWORD   optional: run the round trip with E2EE on
USAGE
  exit 2
fi

export GOJOPLIN_USERNAME="$USERNAME" GOJOPLIN_PASSWORD="$PASSWORD"

RUN="tmp/joplin-server-e2e"
PORT_A=41821
PORT_B=41822
TOKEN_A="e2e-vault-a-token"
TOKEN_B="e2e-vault-b-token"
STAMP="$(date +%Y%m%d-%H%M%S)"
TITLE="server round trip $STAMP"
BODY="body from vault A $STAMP"
EXE=""
if [ "$(go env GOOS)" = "windows" ]; then EXE=".exe"; fi
BIN="tmp/gojoplin-e2e$EXE"
PIDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "logs and vaults are in $RUN" >&2
  exit 1
}

# 1. Is there a Joplin Server answering? /api/ping is the endpoint Joplin's own
# clients use, so a reply also means we are talking to the right software.
ping="$(curl -fsS -m 10 "$URL/api/ping" 2>/dev/null || true)"
case "$ping" in
  *running*|*Joplin*) ;;
  *) fail "no Joplin Server at $URL (GET /api/ping said: ${ping:-nothing}); start one and try again" ;;
esac
echo "server: $URL answered /api/ping"

# 2. Build the CLI under test.
rm -rf "$RUN"
mkdir -p "$RUN/vault-a" "$RUN/vault-b"
go build -o "$BIN" .
echo "built $BIN"

write_config() { # vault-dir, port, token, file
  cat > "$4" <<YAML
sync:
  target: 9
  joplin_server:
    url: "$URL"

api:
  token: "$3"
  master_password: "\${GOJOPLIN_MASTER_PASSWORD}"

server:
  data_dir: "$PWD/$RUN/$1"
  port: $2
  listen_host: "127.0.0.1"

log_level: info
YAML
}
write_config vault-a "$PORT_A" "$TOKEN_A" "$RUN/vault-a.yaml"
write_config vault-b "$PORT_B" "$TOKEN_B" "$RUN/vault-b.yaml"

api() { # port, token, method, path, [json-body]
  local port="$1" token="$2" method="$3" path="$4" body="${5:-}"
  if [ -n "$body" ]; then
    curl -fsS -m 30 -X "$method" -H "Authorization: Bearer $token" \
      -H 'Content-Type: application/json' -d "$body" "http://127.0.0.1:$port$path"
  else
    curl -fsS -m 30 -X "$method" -H "Authorization: Bearer $token" "http://127.0.0.1:$port$path"
  fi
}

start_serve() { # vault-dir, config, port, token, log
  "$BIN" serve --config "$2" >"$5" 2>&1 &
  PIDS+=("$!")
  local i
  for i in $(seq 1 60); do
    if api "$3" "$4" GET /ping >/dev/null 2>&1; then return 0; fi
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

# 3. Vault A: create a notebook and a note through the clipper API, so the note
# goes through the same service/ path the app uses, then sync it up.
start_serve "$RUN/vault-a" "$RUN/vault-a.yaml" "$PORT_A" "$TOKEN_A" "$RUN/vault-a-serve.log" ||
  fail "vault A's API did not come up; see $RUN/vault-a-serve.log"
folder_json="$(api "$PORT_A" "$TOKEN_A" POST /folders "{\"title\":\"E2E $STAMP\"}")" ||
  fail "creating a notebook in vault A"
folder_id="$(printf '%s' "$folder_json" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)"
[ -n "$folder_id" ] || fail "no notebook id in $folder_json"
note_json="$(api "$PORT_A" "$TOKEN_A" POST /notes \
  "{\"title\":\"$TITLE\",\"body\":\"$BODY\",\"parent_id\":\"$folder_id\"}")" ||
  fail "creating a note in vault A"
note_id="$(printf '%s' "$note_json" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)"
[ -n "$note_id" ] || fail "no note id in $note_json"
echo "vault A created notebook $folder_id and note $note_id"
stop_serve

# 4. Push from A and pull into the fresh B, each as a one-shot sync so the
# result is deterministic rather than waiting for a background loop.
"$BIN" sync --config "$RUN/vault-a.yaml" >"$RUN/vault-a-sync.log" 2>&1 ||
  fail "syncing vault A up; see $RUN/vault-a-sync.log"
grep -q "sync completed successfully" "$RUN/vault-a-sync.log" ||
  fail "vault A's sync did not report success; see $RUN/vault-a-sync.log"
echo "vault A pushed to $URL"

"$BIN" sync --config "$RUN/vault-b.yaml" >"$RUN/vault-b-sync.log" 2>&1 ||
  fail "syncing vault B down; see $RUN/vault-b-sync.log"
echo "vault B pulled from $URL"

# 5. Read the note back through vault B's own API: same title, same body.
start_serve "$RUN/vault-b" "$RUN/vault-b.yaml" "$PORT_B" "$TOKEN_B" "$RUN/vault-b-serve.log" ||
  fail "vault B's API did not come up; see $RUN/vault-b-serve.log"
query="$(printf '%s' "$TITLE" | sed 's/ /%20/g')"
found="$(api "$PORT_B" "$TOKEN_B" GET "/search?query=$query")" ||
  fail "searching vault B"
case "$found" in
  *"$TITLE"*) ;;
  *) fail "vault B does not have the note: search returned $found" ;;
esac
read_back="$(api "$PORT_B" "$TOKEN_B" GET "/notes/$note_id?fields=body")" ||
  fail "reading the note from vault B"
case "$read_back" in
  *"$BODY"*) ;;
  *) fail "vault B has the note but a different body: $read_back" ;;
esac
stop_serve

cadence="plaintext"
[ -n "$MASTER_PASSWORD" ] && cadence="E2EE"
echo
echo "PASS: $TITLE round-tripped through $URL ($cadence)"
echo "  notebook $folder_id, note $note_id"
echo "  vault A: $RUN/vault-a (push log $RUN/vault-a-sync.log)"
echo "  vault B: $RUN/vault-b (pull log $RUN/vault-b-sync.log)"
echo "Delete $RUN and the server-side account/data when you are done."
