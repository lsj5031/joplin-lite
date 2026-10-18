#!/usr/bin/env bash
# QA rig: builds the headless binary and runs isolated clipper backends.
# Usage: bash qa/servers.sh start|stop|restart|status
set -uo pipefail
cd "$(dirname "$0")/.."
RUN=qa/run
BIN=$RUN/gojoplin
mkdir -p $RUN/logs

build() {
  if [ ! -x "$BIN" ] || [ -n "$(find . -name '*.go' -newer "$BIN" -not -path './.amp/*' -not -path './reference/*' -print -quit)" ]; then
    echo "building $BIN"
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN" .
  else
    echo "binary is up to date"
  fi
}

start_one() {
  local name=$1 cfg=$RUN/$2 port=$3
  if lsof -nP -iTCP:$port -sTCP:LISTEN >/dev/null 2>&1; then
    echo "$name already listening on $port"
    return 0
  fi
  nohup "$BIN" serve --config "$cfg" --no-sync >"$RUN/logs/$name.log" 2>&1 &
  echo $! > "$RUN/$name.pid"
  for _ in $(seq 1 60); do
    curl -sf "http://127.0.0.1:$port/health" >/dev/null && { echo "$name up on $port"; return 0; }
    sleep 0.25
  done
  echo "FAILED to start $name (see $RUN/logs/$name.log)"; return 1
}

stop_one() {
  local name=$1 port=$2
  if [ -f "$RUN/$name.pid" ]; then kill "$(cat "$RUN/$name.pid")" 2>/dev/null; rm -f "$RUN/$name.pid"; fi
  local pids
  pids=$(lsof -nP -tiTCP:$port -sTCP:LISTEN 2>/dev/null)
  [ -n "$pids" ] && kill $pids 2>/dev/null
  echo "$name stopped"
}

case "${1:-start}" in
  build) build ;;
  start)
    build
    start_one ui ui.yaml 41888
    start_one api api.yaml 41890
    start_one readonly readonly.yaml 41891
    start_one allow allow.yaml 41892
    start_one empty empty.yaml 41893
    ;;
  stop)
    stop_one ui 41888; stop_one api 41890; stop_one readonly 41891; stop_one allow 41892; stop_one empty 41893 ;;
  restart)
    "$0" stop; sleep 0.5; "$0" start ;;
  status)
    for p in 41888 41890 41891 41892 41893; do
      printf "%s " "$p"; curl -s -o /dev/null -w "%{http_code}\n" "http://127.0.0.1:$p/ping" || true
    done ;;
  *) echo "usage: $0 start|stop|restart|status|build"; exit 1 ;;
esac
