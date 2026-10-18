# Real Joplin Server test (sync target 9)

This is the gate for the Joplin Server path: a **real** Joplin Server 3.x, two
throwaway vaults, one note pushed up by the first and pulled down by the
second. Vault B starts empty and never sees vault A's database, so a match
proves the round trip went through the server.

Covers: `POST /api/sessions` login, `info.json` / master-key discovery, the
delta and children listings, item upload (`/api/items/root:/<name>:/content`),
the flat-file-style lock protocol, and (optionally) E2EE push and decrypt.

## 1. Start a throwaway server

Use an empty database and an account that exists only for this test — never
your production server.

Docker (the supported way, SQLite, no PostgreSQL needed):

```sh
docker run --name joplin-e2e -d -p 22300:22300 \
  -e APP_BASE_URL=http://127.0.0.1:22300 \
  -e APP_PORT=22300 \
  -e DB_CLIENT=sqlite3 \
  joplin/server:latest
```

The image seeds `admin@localhost` / `admin`. Create a dedicated account for
the test in the web UI at `http://127.0.0.1:22300` (Admin → Users), or sign in
as the admin account and use it. Sync accounts need the **Sync** permission.

On Windows and macOS the container needs Docker Desktop (WSL 2 backend on
Windows), and installing that is an administrator-level change; a Linux host
or any machine that already runs Docker is easier.

## 2. Run the round trip

```sh
JOPLIN_SERVER_URL=http://127.0.0.1:22300 \
GOJOPLIN_USERNAME=e2e@example.com \
GOJOPLIN_PASSWORD='the password' \
bash qa/joplin-server-e2e.sh
```

Add `GOJOPLIN_MASTER_PASSWORD='the master password'` to run the same round
trip with E2EE: items on the server are then encrypted envelopes and vault B
must decrypt them with that password.

The script prints `PASS` with the notebook and note IDs, or `FAIL` with the
reason. Everything it writes is under `tmp/joplin-server-e2e`: the two vaults,
their configs (`vault-a.yaml`, `vault-b.yaml`) and their logs
(`vault-a-serve.log`, `vault-a-sync.log`, `vault-b-sync.log`,
`vault-b-serve.log`). It leaves them there for inspection; delete the
directory when done.

What it checks, in order:

1. `GET /api/ping` answers — the URL really is a Joplin Server.
2. Vault A's clipper API starts, a notebook and a note are created through
   `service/`, and a one-shot `sync` reports `sync completed successfully`.
3. A fresh vault B syncs down without error.
4. Vault B's API returns the note by search and its body matches what A wrote.

## 3. Optional interop check

Point a **throwaway Joplin desktop profile** (File → Switch profile → New
profile) at the same server, sync, and confirm the note from vault A is there
with the same body; then edit it in Joplin, sync, and add the master password
to the script's environment to pull the encrypted change into a third vault.

## Troubleshooting

- **`no Joplin Server at ...`**: the URL is wrong, the server is still starting
  (watch `docker logs joplin-e2e`), or something else answers on that port.
- **`sync failed: ... 403` / `invalid username or password`**: the account
  exists but the password is wrong, or the account lacks the Sync permission.
- **`sync refused: ... exclusive lock`**: another client is syncing that
  account. Wait for the lock TTL and retry, and do not run two clients that
  share one account at the same time.
- **`sync target format is newer than this client supports`**: the server is a
  newer major version than this build understands; use a matching server.
- **Decryption failures with E2EE**: the master password must match the
  account's exactly (no trimming).
- The script's `kill` of its `serve` processes is a plain kill; if a port is
  left busy, `taskkill /IM gojoplin-e2e.exe /F` (Windows) or `pkill -f
  gojoplin-e2e` clears it.
