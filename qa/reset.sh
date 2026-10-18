#!/usr/bin/env bash
# Reset + reseed the disposable QA backends.
#  - db-ui     seeded with the UI fixture library (qa/seed_ui_data.py)
#  - db-api    seeded with a few notes/folders/tags (qa/seed_backend.py api)
#  - db-ro     seeded with one note (so read-only refusals can be exercised)
#  - db-allow  seeded with "Allowed"/"Other", "green"/"red", one note
#  - db-empty  empty
set -euo pipefail
cd "$(dirname "$0")/.."
bash qa/servers.sh stop >/dev/null 2>&1 || true
sleep 0.5
rm -rf qa/run/db-ui qa/run/db-api qa/run/db-ro qa/run/db-allow qa/run/db-empty
mkdir -p qa/run/logs
bash qa/servers.sh build >/dev/null

# The api/ro/allow databases must be seeded while a temporary wildcard policy
# points at them, because their real policies cannot create folders/tags.
seed_backend() { # <datadir> <kind>
  cat > qa/run/_seed.yaml <<YAML
sync:
  target: 7
api:
  token: seed-token
  key: seed-key
server:
  data_dir: $(pwd)/qa/run/$1
  listen_host: 127.0.0.1
  port: 41894
mcp:
  allow_folders: "*"
  allow_tags: "*"
  allow_create_folder: true
  allow_create_tag: true
observability:
  tracing:
    enabled: false
  metrics:
    enabled: false
YAML
  nohup qa/run/gojoplin serve --config qa/run/_seed.yaml --no-sync >"qa/run/logs/seed-$1.log" 2>&1 &
  local pid=$!
  for _ in $(seq 1 60); do
    curl -sf http://127.0.0.1:41894/health >/dev/null && break
    sleep 0.25
  done
  python3 qa/seed_backend.py 41894 seed-token "$2"
  kill "$pid" 2>/dev/null || true
  sleep 0.3
}
seed_backend db-api api
seed_backend db-ro ro
seed_backend db-allow allow
rm -f qa/run/_seed.yaml

bash qa/servers.sh start
python3 qa/seed_ui_data.py
echo "seeded UI backend: http://127.0.0.1:41888 (token ui-token)"
