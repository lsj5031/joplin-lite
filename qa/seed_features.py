#!/usr/bin/env python3
"""Seeds qa/features.csv — the single canonical spreadsheet of user stories.

Each row is one user story with the behavior the code promises. Run once;
afterwards edit the CSV (or qa/track.py), never this script, unless re-seeding.
"""
import csv
import os

QA = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(QA, "features.csv")

ROWS = []
COUNTER = {}


def r(area, feature, story, expected, layer, note=""):
    COUNTER[area] = COUNTER.get(area, 0) + 1
    prefix = {
        "Shell": "SH", "Workspace UI": "UI", "Data API": "API", "Sync": "SYNC",
        "Storage & format": "MOD", "CLI & config": "CLI", "Service seam": "SVC",
        "Observability & extras": "OBS",
    }[area]
    ROWS.append({
        "id": f"{prefix}-{COUNTER[area]:02d}",
        "area": area,
        "feature": feature,
        "user_story": story,
        "expected": expected,
        "status": "untested",
        "severity": "-",
        "layer": layer,
        "evidence": "",
        "error": "",
        "fix": "",
        "notes": note,
        "updated": "",
    })


# ---------------------------------------------------------------- Shell
r("Shell", "Launch window", "As a user I start the desktop app and get a native window.",
  "A window titled 'Joplin Lite' opens at 1400x900, resizable, minimum 1024x640.", "shell")
r("Shell", "Embedded UI", "As a user the app shows the workspace without a Node server.",
  "index.html and hashed assets are served from the embedded webdist over app://localhost.", "shell")
r("Shell", "Runtime config injection", "As the page I receive API coordinates before the bundle runs.",
  "window.__GOJOPLIN__ = {base, token, shell:'desktop', profile} is injected before </head>.", "shell")
r("Shell", "In-process API", "As a user I get one program with one lifecycle.",
  "The clipper/Data API binds before the window opens; no separate process or Node runtime.", "shell")
r("Shell", "Ephemeral token", "As a user without a configured api.token I still get a working UI.",
  "A random 48-hex token is minted per session, never persisted, and never logged; a warning is logged.", "shell")
r("Shell", "Single instance", "As a user a second launch focuses the running app.",
  "The second process exits and shows/focuses the existing window instead of opening another.", "shell")
r("Shell", "Window state", "As a user my window size and position are remembered.",
  "MyGO restores the window geometry for StateKey 'main' on the next launch.", "shell")
r("Shell", "Close lifecycle", "As a user closing the window shuts everything down.",
  "Closing the window stops the HTTP server and closes SQLite; Close is idempotent.", "shell")
r("Shell", "Startup failure", "As a user I am told when the API cannot start.",
  "A native error dialog names the bind failure and points at port 41184; the app then quits.", "shell")
r("Shell", "Clipper pairing", "As a user I decide whether a browser extension may pair.",
  "A native Accept/Reject dialog appears; Reject is the default and no prompt means no grant.", "shell")
r("Shell", "App icon", "As a user the app shows its own icon in the dock.",
  "The embedded PNG is applied via Dock.SetIcon; failures are logged, not fatal.", "shell")
r("Shell", "Smoke mode", "As a developer I can verify the real webview in CI.",
  "-smoke evaluates the live page, calls the API (GET + POST/DELETE), writes tmp/desktop-smoke.{json,png}, then quits.", "shell")
r("Shell", "SPA fallback & caching", "As a user deep links work and assets are cached correctly.",
  "Extensionless paths serve index.html with no-cache; /assets/* are immutable; missing assets 404; writes are rejected.", "shell")
r("Shell", "No-sync mode", "As a user I can run local-only.",
  "-no-sync serves and edits the local database with no remote, no sync loop, and no lock work.", "shell")
r("Shell", "Address override", "As a user I can move the local API off 41184.",
  "-addr <host:port> overrides server.port/listen_host and the page's injected base follows it.", "shell")
r("Shell", "Data dir override", "As a user I can point the app at another database.",
  "-data-dir overrides server.data_dir; the profile key (recovery namespace) follows the resolved path.", "shell")

# ---------------------------------------------------------------- Workspace UI
r("Workspace UI", "Initial load", "As a user I open the workspace and see my library.",
  "Notebooks, tags and the newest 50 note summaries load; footer says 'Saved locally'; no synthetic data appears.", "ui")
r("Workspace UI", "Empty library", "As a new user with no notes I get a clear first step.",
  "The list shows 'Your library is ready for its first note' with a Create button that starts a note.", "ui")
r("Workspace UI", "Create note", "As a user I press New note and start writing.",
  "A note titled 'Untitled note' is created through the API, appears first, is selected, and its body is editable.", "ui")
r("Workspace UI", "Create in notebook", "As a user I create a note while a notebook is selected.",
  "The new note is created in that notebook (parent_id), which stays selected in the sidebar.", "ui")
r("Workspace UI", "Create during tag filter", "As a user I create a note while a tag filter is active.",
  "The tag filter clears, the new note is selected, and the list returns to the unfiltered notebook scope.", "ui")
r("Workspace UI", "Body autosave", "As a user my typing is saved without a Save button.",
  "Body edits save after ~600 ms; the footer shows Saving locally… then Saved locally; the stored body matches.", "ui")
r("Workspace UI", "Reload durability", "As a user I can reload immediately after typing and keep my text.",
  "Pending edits are journaled synchronously to localStorage and flushed on load, so an instant reload loses nothing.", "ui")
r("Workspace UI", "Title edit", "As a user I rename a note.",
  "Manage note exposes a required title field; saving updates the header, the list row and the API.", "ui")
r("Workspace UI", "Move note", "As a user I move a note between notebooks.",
  "Manage note's Notebook select updates parent_id; notebook name and counts update after save.", "ui")
r("Workspace UI", "Assign tag", "As a user I attach an existing tag.",
  "Manage note checkboxes attach the tag; the chip appears in the header and beside the list row.", "ui")
r("Workspace UI", "Create tag inline", "As a user I create a tag while organizing a note.",
  "New tag is created (reusing an existing title case-insensitively), attached, and shown in the sidebar.", "ui")
r("Workspace UI", "Remove tag", "As a user I detach a tag from a note.",
  "Unchecking and saving removes the association; the chip disappears and the API no longer lists it.", "ui")
r("Workspace UI", "Delete note", "As a user I delete a note deliberately.",
  "Delete note asks for confirmation, 'Delete permanently' removes it, the row disappears and GET returns 404.", "ui")
r("Workspace UI", "Cancel delete", "As a user I can back out of deleting.",
  "'Keep note' closes the confirmation and leaves the note and its body untouched.", "ui")
r("Workspace UI", "Manage cancel", "As a user I can discard unsubmitted details.",
  "Cancel closes Manage note without changing title, notebook or tags.", "ui")
r("Workspace UI", "Todo toggle", "As a user I complete a to-do note.",
  "The toggle writes todo_completed, updates the header state and the list icon color without losing the body.", "ui")
r("Workspace UI", "Todo due", "As a user I see when an open to-do is due.",
  "An incomplete to-do with todo_due shows '· due <date>' in the list; completed ones do not.", "ui")
r("Workspace UI", "Markdown preview", "As a user I can preview a note.",
  "Preview renders headings, emphasis, lists, code blocks and links from the Markdown source.", "ui")
r("Workspace UI", "Preview sanitization", "As a user a malicious note cannot run script.",
  "Raw HTML is not rendered; script/img handlers are removed by DOMPurify while plain Markdown still renders.", "ui")
r("Workspace UI", "Task list preview", "As a user I see task checkboxes in preview.",
  "'- [ ]' and '- [x]' list items render as disabled, labelled checkboxes; other brackets stay text.", "ui")
r("Workspace UI", "Conflict banner", "As a user I am told when both sides changed a note.",
  "A conflict note shows the 'nothing was overwritten' banner with the kept-copy explanation.", "ui")
r("Workspace UI", "Conflict actions", "As a user I can act on a conflict from the banner.",
  "'Open synced version' navigates to the synced original; 'Keep my copy' dismisses the banner. No dead controls.", "ui")
r("Workspace UI", "Search title", "As a user I find a note by its title.",
  "Typing part of a title lists matching notes and the list header names the query.", "ui")
r("Workspace UI", "Search body", "As a user I find a note by words in its body.",
  "Body-only terms match through the FTS index, case-insensitively.", "ui")
r("Workspace UI", "Search debounce", "As a user search does not fire per keystroke.",
  "A 250 ms pause produces one /search request for the accumulated query.", "ui")
r("Workspace UI", "No results", "As a user I understand an empty search.",
  "The list says 'No notes match “q”' and offers Clear search, which restores the library.", "ui")
r("Workspace UI", "Escape search", "As a user Esc clears search quickly.",
  "Escape empties the query and restores the unfiltered list (except while a dialog is open).", "ui")
r("Workspace UI", "Focus search", "As a user Cmd/Ctrl+K jumps to search.",
  "The keyboard shortcut focuses the search field and prevents the browser's own binding.", "ui")
r("Workspace UI", "Notebook filter", "As a user I scope the list to a notebook.",
  "Selecting a notebook lists only its notes, titles the list with the notebook, and marks it current.", "ui")
r("Workspace UI", "Nested notebooks", "As a user I navigate arbitrarily deep notebook trees.",
  "Every level renders indented with a guide line; a note in a third-level notebook is reachable by clicking it.", "ui")
r("Workspace UI", "Create notebook", "As a user I add a top-level notebook.",
  "New notebook creates it with no parent, closes the form, and shows it in the sidebar.", "ui")
r("Workspace UI", "Create child notebook", "As a user I nest a new notebook.",
  "Choosing a parent in the form creates the notebook under it and it renders at the next level.", "ui")
r("Workspace UI", "Notebook counts", "As a user I see how many notes each notebook holds.",
  "With the full library loaded, each notebook shows its exact note count (descendants included).", "ui")
r("Workspace UI", "Paged counts", "As a user I am not shown a wrong total.",
  "When only a page is loaded, per-notebook/tag counts are omitted and All notes shows '+' instead of a false total.", "ui")
r("Workspace UI", "Tag filter", "As a user I scope the list to a tag.",
  "Selecting a tag lists only tagged notes and titles the list 'Tag: <name>'.", "ui")
r("Workspace UI", "Tag counts", "As a user I see tag note counts when they are trustworthy.",
  "Counts match the loaded full library; they are hidden on a paginated partial load.", "ui")
r("Workspace UI", "Load more", "As a user I can page past the first 50 notes.",
  "Load more appends the next keyset page with no duplicate rows and hides at the end.", "ui")
r("Workspace UI", "Keyboard navigation", "As a user I move through the list without the mouse.",
  "Up/Down/Home/End move focus (not selection), clamped at the ends; Tab leaves the list; Enter opens the focused note.", "ui")
r("Workspace UI", "Selection fetch", "As a user opening a note loads its full body.",
  "The list ships summaries only; selecting a row issues GET /notes/{id} and the editor shows that body.", "ui")
r("Workspace UI", "Row metadata", "As a user I can judge a note from its row.",
  "Rows show title, excerpt, notebook, tags, updated time, and an amber due date for open to-dos.", "ui")
r("Workspace UI", "Excerpt formatting", "As a user excerpts are readable.",
  "The first non-empty line has leading Markdown markers stripped, whitespace collapsed and is capped at 140 runes.", "ui")
r("Workspace UI", "Reload button", "As a user I can explicitly refresh from disk.",
  "Reload shows a busy state, flushes pending saves, and refetches metadata, the current page and the open note.", "ui")
r("Workspace UI", "Save states", "As a user I always know whether my work is stored.",
  "Footer states are Saved locally / Saving locally… / Local save failed, reflecting the real write state.", "ui")
r("Workspace UI", "Disconnected", "As a user I never mistake a failed backend for demo data.",
  "When the API is unreachable the app shows an error/retry state, disables editing, and does not substitute mock notes.", "ui")
r("Workspace UI", "Reconnect", "As a user Retry restores the workspace.",
  "Retry reconnects, reloads metadata and the list, and re-enables editing without a page reload.", "ui")
r("Workspace UI", "Save failure", "As a user a failed save is visible and recoverable.",
  "A failed write surfaces an alert and the failed footer state; the text stays in the editor and a retry can persist it.", "ui")
r("Workspace UI", "Narrow layout", "As a phone-width user I get one pane at a time.",
  "Below 1024 px the list and editor swap, Navigation opens as a dialog, and Back returns to the list.", "ui")
r("Workspace UI", "Minimum desktop", "As a laptop user I keep all three panes.",
  "At 1024x640 the sidebar, list and editor are all visible and the page does not scroll horizontally.", "ui")
r("Workspace UI", "Theme choice", "As a user I choose light, dark or system.",
  "The choice persists across reloads and immediately changes the rendered theme.", "ui")
r("Workspace UI", "System theme", "As a user on 'System' the app follows OS changes.",
  "A prefers-color-scheme change re-applies the theme while the explicit choice still overrides it.", "ui")
r("Workspace UI", "Settings dialog", "As a user I can see what the app is doing locally.",
  "Settings identifies Local workspace, explains that sync/E2EE are configured outside the UI, and closes with Escape.", "ui")
r("Workspace UI", "Accessibility", "As a keyboard/screen-reader user every control is usable.",
  "Icon-only buttons have names, controls are >=44 px touch targets where space allows, and focus returns after dialogs.", "ui")
r("Workspace UI", "Truncation", "As a user long titles and bodies do not break the layout.",
  "Long titles/excerpts truncate with ellipsis and the document has no horizontal overflow at 390/1024/1440 px.", "ui")
r("Workspace UI", "Header metadata", "As a user I see where the open note lives.",
  "The editor header shows notebook, updated time and one chip per tag.", "ui")
r("Workspace UI", "Delete selection", "As a user deleting the open note selects something sensible.",
  "The next row becomes active, or the editor shows the empty state when none remain.", "ui")
r("Workspace UI", "Reload selection", "As a user a reload keeps my place.",
  "If the open note still exists after Reload, it stays selected and its body is refreshed.", "ui")
r("Workspace UI", "Tag filter toggle", "As a user I can clear a tag filter by clicking it again.",
  "Clicking the active tag deselects it and returns to All notes.", "ui")
r("Workspace UI", "Notebook create validation", "As a user I cannot create a nameless notebook.",
  "The Create notebook button stays disabled for an empty/whitespace title; Escape/Cancel abandons the form.", "ui")
r("Workspace UI", "Tag creation reuse", "As a user I do not get duplicate tags from case differences.",
  "A new tag whose title matches an existing tag case-insensitively reuses that tag.", "ui")
r("Workspace UI", "Server error messages", "As a user API errors are readable.",
  "Errors are shown as 'METHOD /path → status: message', not a raw stack or silent failure.", "ui")
r("Workspace UI", "Offline recovery", "As a user an edit made while the API blips is not lost.",
  "The journaled edit is retried on the browser 'online' event and on the next save/reload.", "ui")
r("Workspace UI", "No console errors", "As a user the workspace loads without errors.",
  "First load produces no uncaught page errors, unhandled rejections, or hydration warnings.", "ui")

# ---------------------------------------------------------------- Data API
r("Data API", "Ping", "As a Joplin client I can discover the clipper.",
  "GET /ping returns text/plain 'JoplinClipperServer' with 200, unauthenticated.", "api")
r("Data API", "Health", "As an operator I can probe liveness.",
  "GET /health returns 200 with no body and no auth.", "api")
r("Data API", "Token required", "As a user my data is not readable without the token.",
  "Any non-open route without ?token= or Bearer returns 403 with a JSON error.", "api")
r("Data API", "Invalid token", "As a user a wrong token is rejected.",
  "?token=wrong returns 403 'Invalid \"token\" parameter'.", "api")
r("Data API", "Bearer auth", "As CLI tooling I can use a bearer key.",
  "Authorization: Bearer <api.key or api.token> is accepted on protected routes.", "api")
r("Data API", "Pairing request", "As the web clipper I start pairing.",
  "POST /auth from a non-web origin returns {'auth_token': <32 hex>} and status becomes waiting.", "api")
r("Data API", "Pairing waiting", "As the extension I poll pairing state.",
  "GET /auth/check?auth_token=<x> returns {'status':'waiting'} until a decision exists.", "api")
r("Data API", "Pairing accept", "As a user I approve pairing.",
  "Accepting resolves the request and /auth/check returns accepted plus the API token.", "api")
r("Data API", "Pairing reject", "As a user I refuse pairing.",
  "Rejecting resolves it to rejected and no token is ever returned.", "api")
r("Data API", "Pairing origin", "As a user a web page cannot pair itself.",
  "POST /auth and /auth/check from an http(s) Origin return 403; extension/native origins are allowed.", "api")
r("Data API", "Token validation", "As the extension I can verify a stored token.",
  "GET /auth/check?token=<x> returns {'valid': true|false}; without params it is 400 'Invalid request'.", "api")
r("Data API", "Notes list", "As a client I list notes newest first.",
  "GET /notes returns up to 10 full notes with has_more, ordered by updated_time desc.", "api")
r("Data API", "Pagination cap", "As a client I cannot ask for unbounded pages.",
  "limit is capped at 100 and page>0 selects the offset; has_more reflects the extra row.", "api")
r("Data API", "Fields projection", "As a client I can request only some fields.",
  "fields=id,title returns objects with exactly those keys, for lists and single items.", "api")
r("Data API", "Ordering", "As a client I can order by an allowed column.",
  "order_by=title&order_dir=asc sorts by title; unknown columns fall back to updated_time and unknown dirs to DESC.", "api")
r("Data API", "Summary mode", "As the webview I fetch rows without bodies.",
  "summary=1 returns excerpt (<=140 runes), tag_ids and no body field.", "api")
r("Data API", "Summary cursor", "As the webview I page summaries by keyset.",
  "next_cursor is returned and passing cursor= yields the next page without duplicates or skipped rows.", "api")
r("Data API", "Conflicts flag", "As a client I choose whether conflicts are listed.",
  "Conflict notes are excluded by default and included with include_conflicts=1.", "api")
r("Data API", "Notes filters", "As a client I can scope /notes.",
  "parent_id, tag_id and query (with summary=1) filter the list; query uses the FTS index.", "api")
r("Data API", "Create note", "As a clipper I create a note with minimum fields.",
  "POST /notes fills id, created/updated/user times, markup_language=1, type_=1 and returns the note.", "api")
r("Data API", "Create with tags", "As a clipper I attach tags at creation.",
  "tag_ids attaches existing tags; an unknown tag returns 400 and no note is created.", "api")
r("Data API", "Create body_html", "As a clipper I can send HTML instead of Markdown.",
  "body_html on POST /notes is converted to Markdown (Joplin Data API behavior).", "api")
r("Data API", "Get note 404", "As a client I get a clean miss.",
  "GET /notes/<missing> returns 404 {'error':'not found'}.", "api")
r("Data API", "Update note", "As a clipper I update note fields.",
  "PUT accepts title, body, parent_id, is_todo, todo_completed, todo_due, source_url, author, markup_language, order and returns the stamped note.", "api")
r("Data API", "Update body_html", "As a clipper I can update from HTML.",
  "PUT body_html replaces the body with converted Markdown; empty HTML leaves the body alone.", "api")
r("Data API", "Update 404", "As a client I cannot update a missing note.",
  "PUT /notes/<missing> returns 404 before any policy or JSON work.", "api")
r("Data API", "Delete note", "As a clipper I remove a note.",
  "DELETE /notes/{id} returns {'status':'ok'}, the note is gone, and the deletion is queued for sync.", "api")
r("Data API", "Folders CRUD", "As a client I manage notebooks.",
  "GET/POST/GET id/PUT/DELETE /folders create, rename, re-parent and delete folders with 404s for misses.", "api")
r("Data API", "Folder tree", "As a client I fetch the notebook tree.",
  "GET /folders?as_tree=1 returns folders with nested children arrays.", "api")
r("Data API", "Folder notes", "As a client I list a notebook's notes.",
  "GET /folders/{id}/notes supports limit/page/order/fields/include_conflicts and summary mode.", "api")
r("Data API", "Tags CRUD", "As a client I manage tags.",
  "GET/POST/GET id/PUT/DELETE /tags work with 404s for misses.", "api")
r("Data API", "Attach tag", "As a clipper I tag a note.",
  "POST /tags/{id}/notes {'id': noteId} links them; repeating is idempotent.", "api")
r("Data API", "Detach tag", "As a clipper I untag a note.",
  "DELETE /tags/{id}/notes/{noteId} removes the link and queues the link deletion.", "api")
r("Data API", "Note tags", "As a client I list a note's tags.",
  "GET /notes/{id}/tags returns full tag entities with pagination and fields support.", "api")
r("Data API", "Tag notes", "As a client I list notes for a tag.",
  "GET /tags/{id}/notes returns tagged notes with paging/fields, and summary mode when asked.", "api")
r("Data API", "Search notes", "As a client I search note text.",
  "GET /search?query= matches titles and bodies via FTS5; type defaults to note.", "api")
r("Data API", "Search folders", "As a client I search notebook names.",
  "type=folder matches a folder-title prefix case-insensitively.", "api")
r("Data API", "Search tags", "As a client I search tag names.",
  "type=tag matches a tag-title prefix case-insensitively.", "api")
r("Data API", "Search validation", "As a client I get a clear error for no query.",
  "query is required (400); a query with no matches returns an empty items list, not an error.", "api")
r("Data API", "Search safety", "As a client FTS syntax in a query cannot break the server.",
  "Quotes, *, parentheses and colons are handled without 500s and still return sensible matches.", "api")
r("Data API", "Resource upload", "As a clipper I upload an attachment.",
  "POST /resources multipart (data + optional props/title) stores metadata and bytes; GET /resources/{id}/file returns them with type/filename.", "api")
r("Data API", "Resource metadata", "As a client I update or delete a resource.",
  "PUT changes title/filename/mime; DELETE removes metadata and queues the deletion.", "api")
r("Data API", "Resource policy", "As an operator the read-only policy also covers attachments.",
  "With no writable folders, POST/PUT/DELETE /resources are refused like note mutations.", "api")
r("Data API", "Revisions", "As a client I can read note history.",
  "GET /revisions lists raw revision metadata, GET /{id} returns one or 404, DELETE removes it.", "api")
r("Data API", "Events", "As a client I can poll the change feed.",
  "GET /events?cursor= returns item changes with has_more; GET /events/{id} returns one or 404.", "api")
r("Data API", "CORS", "As the desktop page I can call the API cross-origin.",
  "Preflight from app://localhost is allowed for GET/POST/PUT/DELETE with Content-Type/Authorization.", "api")
r("Data API", "Bad JSON", "As a client malformed input is rejected cleanly.",
  "POST/PUT with invalid JSON return 400 'invalid JSON: …' and change nothing.", "api")
r("Data API", "Read-only policy", "As an operator I can run the server read-only.",
  "With an empty allow-list, creating notes/folders/tags and mutating notes returns 403 with a configuration hint.", "api")
r("Data API", "Writable allow-list", "As an operator I can allow specific notebooks/tags.",
  "By title or ID, only allow-listed folders accept note creates/updates and only allow-listed tags can be attached.", "api")
r("Data API", "Orphan note guard", "As an operator bad parent IDs are refused.",
  "Creating or moving a note into a nonexistent folder returns 400 instead of storing an orphan.", "api")
r("Data API", "Unicode round-trip", "As a user international text survives the API.",
  "Emoji, CJK, combining marks and multi-KB bodies round-trip byte-identically through create/get/update.", "api")
r("Data API", "Unknown routes", "As a client bad requests fail predictably.",
  "Unknown paths return 404; unsupported methods return 405 (or chi's method response), never a panic.", "api")
r("Data API", "Revision rewrite refused", "As a user note history is immutable through the API.",
  "PUT /revisions/{id} is rejected (405/400) and never rewrites stored history; missing ids 404.", "api")

# ---------------------------------------------------------------- Sync
r("Sync", "Delta pull", "As a user changes made elsewhere arrive here.",
  "PullChanges applies remote creates/updates/deletes from the delta cursor into the local store.", "sync")
r("Sync", "Push local", "As a user my local changes reach the target.",
  "PushChanges uploads dirty notes, folders, tags, note-tags and resources, then stamps per-item sync times.", "sync")
r("Sync", "Deletion propagation", "As a user deleting here deletes there.",
  "Deletions queued in deleted_items are sent to the target and removed from the queue.", "sync")
r("Sync", "Remote delete", "As a user a remote delete removes a clean local note.",
  "A note with no local changes is deleted locally when the target reports it gone.", "sync")
r("Sync", "Double-edit conflict", "As a user simultaneous edits never lose data.",
  "The local version is kept as a new conflict note (is_conflict=1, original id set) and the remote stays canonical.", "sync")
r("Sync", "Deleted-while-edited", "As a user a remote delete does not discard my edits.",
  "The modified local note is first kept as a conflict copy, then the original is deleted.", "sync")
r("Sync", "Folder delete conflict", "As a user losing a notebook does not orphan notes.",
  "Notes in a remotely deleted folder are marked is_conflict rather than silently orphaned.", "sync")
r("Sync", "Attachment conflict", "As a user two edited attachments keep both.",
  "The local blob is duplicated with a new id and an 'Attachment conflict' note is filed in 'Conflicts (attachments)'.", "sync")
r("Sync", "Resource blob conflict", "As a user a remotely changed attachment does not clobber unsynced bytes.",
  "An unpushed local blob is copied to a conflict resource before the remote blob becomes canonical.", "sync")
r("Sync", "Lock lifecycle", "As a user two clients do not sync at once.",
  "A lock is acquired before a pass and released after it, with a 3-minute TTL on file targets.", "sync")
r("Sync", "Exclusive lock", "As a user a locked target refuses my sync.",
  "A held exclusive lock makes the pass fail with 'sync refused' instead of proceeding unlocked.", "sync")
r("Sync", "Lock refresh", "As a user long syncs keep their lock.",
  "The held lock is refreshed every 60 s; an expired lock stops the refresh and fails.", "sync")
r("Sync", "Target version", "As a user I cannot sync with a newer format.",
  "info.json version > 3 or appMinVersion > 3.7.0 refuses the sync with ErrSyncTargetUnsupported.", "sync")
r("Sync", "Master keys pull", "As a user E2EE keys from other clients are learned.",
  "Master keys in info.json are stored locally; active_master_key_id is recorded.", "sync")
r("Sync", "Decrypt pulled", "As a user I can read E2EE notes from Joplin.",
  "Pulled encrypted notes/resources are decrypted with the master password and stored as plaintext plus raw.", "sync")
r("Sync", "Encrypt push", "As a user my pushed notes stay encrypted on the target.",
  "When a master key is active, pushed items are encrypted (JED01 envelope) with the active key.", "sync")
r("Sync", "Sync point", "As a user each target tracks what it has seen.",
  "Per-item, per-target sync times advance only after a successful push, so redelivery does not echo.", "sync")
r("Sync", "Reconcile", "As a user items the delta missed are recovered.",
  "PullMissingItems fetches master keys/items not present in the delta and prunes stale local items.", "sync")
r("Sync", "Overlap coalescing", "As a user concurrent triggers do not double-sync.",
  "A trigger during a running pass is ignored (try-lock), never queued into a second overlapping pass.", "sync")
r("Sync", "Local round-trip", "As a user a local folder target proves the engine end to end.",
  "Two profiles syncing through a folder converge: edits, tags, deletes and conflicts match Joplin semantics.", "sync")
r("Sync", "Dropbox backend", "As a Dropbox user I can sync through my own app folder.",
  "OAuth token refresh, upload/download/delta/children, locks and path mapping work against the API.", "sync")
r("Sync", "Dropbox OAuth", "As a Dropbox user I can authorize without copying a token.",
  "PKCE authorization-code flow on a loopback redirect stores and refreshes dropbox_token.json.", "sync")
r("Sync", "Joplin Server", "As a Joplin Server user I can sync with my server.",
  "Session auth, /api/items paths and server locks work against a real Joplin Server.", "sync")
r("Sync", "S3 build tag", "As an operator S3 is opt-in at build time.",
  "The default binary's S3 backend is a stub with an explanatory error; -tags s3 links the real backend.", "sync")

# ---------------------------------------------------------------- Storage & format
r("Storage & format", "Golden corpus", "As a user Joplin items round-trip losslessly.",
  "Every golden fixture deserializes and re-serializes byte-identically.", "store")
r("Storage & format", "Unknown fields", "As a user metadata from newer Joplin versions survives.",
  "Unknown keys keep their exact text and position through the raw passthrough.", "store")
r("Storage & format", "Typed metadata", "As a client metadata values have the right JSON types.",
  "Numeric/boolean known keys are emitted typed; other values stay strings.", "store")
r("Storage & format", "Raw items", "As a user my bytes are kept next to the typed row.",
  "item_raw stores the exact serialized item and is pushed verbatim until the item changes.", "store")
r("Storage & format", "Fresh migration", "As a new user the database is created correctly.",
  "Opening an empty directory migrates to schema 49 and enables WAL.", "store")
r("Storage & format", "Joplin profile", "As a user I can open a real Joplin profile.",
  "A Joplin-like database is migrated additively (sidecar tables only), leaving core tables intact.", "store")
r("Storage & format", "FTS freshness", "As a user search reflects edits immediately.",
  "Create/update/delete keeps jl_notes_fts in sync so search never returns deleted or stale text.", "store")
r("Storage & format", "Settings KV", "As a client settings persist.",
  "GetSetting/SetSetting round-trip values in the database.", "store")
r("Storage & format", "Conflict listing", "As the UI I can list conflicts.",
  "ListConflictNotes returns only is_conflict=1 notes, newest first.", "store")
r("Storage & format", "Revisions store", "As a client revisions survive restart.",
  "UpsertRevision/ListRevisions/CountRevisions/DeleteRevision persist index rows and raw metadata.", "store")
r("Storage & format", "Resource files", "As a user attachments live on disk.",
  "SaveResourceFile/GetResourceFile round-trip bytes under the resources directory.", "store")
r("Storage & format", "Deletion queue", "As a user deletions reach every target that saw the item.",
  "recordDeletion inserts one row per target only for items that were actually synced.", "store")
r("Storage & format", "Event log", "As a client the change feed is append-only.",
  "Each note/folder/tag mutation appends an item_changes row, and reads page by cursor.", "store")

# ---------------------------------------------------------------- CLI & config
r("CLI & config", "Help", "As an operator I can discover commands.",
  "gojoplin --help lists serve, sync, config, dropbox-auth and clip with short descriptions.", "cli")
r("CLI & config", "Config print", "As an operator I can inspect resolved settings safely.",
  "config prints target, URL, user, redacted secrets, data dir, listen address and observability.", "cli")
r("CLI & config", "Serve token required", "As an operator serve refuses to run unprotected.",
  "Missing api.token exits with 'api token is required' and a configuration hint.", "cli")
r("CLI & config", "Serve key required", "As an operator serve refuses to run without a clipper key.",
  "Missing api.key exits with 'api key is required'.", "cli")
r("CLI & config", "Serve local-only", "As an operator I can run serve without a remote.",
  "serve --no-sync opens the DB, starts the API and does no remote work.", "cli")
r("CLI & config", "Serve listen", "As an operator the server binds and announces itself.",
  "It logs the configured address and the API token, and serves /ping.", "cli")
r("CLI & config", "One-shot sync", "As an operator I can sync once from cron.",
  "sync opens the DB, builds the configured backend and runs one engine pass, failing loudly.", "cli")
r("CLI & config", "Clip ping", "As a script I can check the API.",
  "clip ping sends Bearer auth and prints the ping body.", "cli")
r("CLI & config", "Clip notes", "As a script I can manage notes.",
  "clip notes list/get/create/delete round-trip through the HTTP API.", "cli")
r("CLI & config", "Clip folders", "As a script I can list and create notebooks.",
  "clip folders list/create return the API entities.", "cli")
r("CLI & config", "Clip tags", "As a script I can list tags.",
  "clip tags list returns tags from the API.", "cli")
r("CLI & config", "Clip search", "As a script I can search.",
  "clip search <query> prints matching notes.", "cli")
r("CLI & config", "Dropbox auth", "As a Dropbox user I can run the OAuth flow from the CLI.",
  "dropbox-auth prints a URL, waits on the loopback redirect, stores the token and reports the account.", "cli")
r("CLI & config", "YAML env expansion", "As an operator secrets stay out of files.",
  "${VAR} in api.token/key/master_password and the OTLP endpoint expands from the environment.", "cli")
r("CLI & config", "Joplin settings import", "As a Joplin user I can reuse my settings.json.",
  "Flat dotted keys map to the same Config; missing sync target defaults and observability are applied.", "cli")
r("CLI & config", "Env precedence", "As an operator env overrides the file.",
  "GOJOPLIN_* overrides and CLI overrides win over the YAML values for secrets, port and data dir.", "cli")
r("CLI & config", "Validation", "As an operator misconfiguration fails fast.",
  "Unsupported target, missing S3 bucket/URL/region, missing server URL or empty username are rejected with specific errors.", "cli")
r("CLI & config", "No-sync validation", "As an operator -no-sync does not demand a remote.",
  "No-sync skips sync-target validation so a local-only config loads without S3/server fields.", "cli")
r("CLI & config", "Data dir default", "As a user I get a sane default location.",
  "No server.data_dir falls back to GOJOPLIN_DATA_DIR or ~/.local/share/gojoplin.", "cli")
r("CLI & config", "Listen defaults", "As a user the API is local by default.",
  "Default listen host is localhost (not 0.0.0.0) and port 41184; both can be overridden.", "cli")
r("CLI & config", "Port collision", "As a user a busy port is a clear error.",
  "Serve returns a socket-in-use error rather than silently choosing another port.", "cli")
r("CLI & config", "Completion scripts", "As an operator I can generate shell completion.",
  "the `completion` command emits a bash/zsh/fish/powershell script (cobra-generated).", "cli")

# ---------------------------------------------------------------- Observability & extras
r("Observability & extras", "Tracing config", "As an operator I can configure OTLP tracing.",
  "observability.tracing parses and validates; when enabled the app warns that span export is not implemented (no silent no-op).", "cli")
r("Observability & extras", "Metrics config", "As an operator I can configure Prometheus metrics.",
  "observability.metrics parses and validates; when enabled the app warns that no /metrics endpoint is served yet.", "cli")
r("Observability & extras", "RAG config", "As an operator I can configure semantic search.",
  "rag parses and validates; when enabled the app warns that embedding search is not implemented and search stays FTS.", "cli")
r("Observability & extras", "MCP tool filter", "As an operator I can restrict exposed MCP tools.",
  "mcp.enabled_tools parses; a non-'*' value is reported as not implemented instead of silently ignored.", "cli")
r("Observability & extras", "Example config honesty", "As a new user the example config matches real behavior.",
  "config.yaml.example disables and annotates the not-yet-implemented observability/rag/mcp-tool settings, so copying it cannot enable a silent no-op.", "cli")

# ---------------------------------------------------------------- Service seam
r("Service seam", "List notes", "As a Go caller I can list notes through the typed seam.",
  "ListNotes applies Query > ConflictOnly > FolderID precedence and returns has_more.", "service")
r("Service seam", "Summaries", "As a Go caller I can page summaries by keyset.",
  "ListNoteSummaries returns rows without bodies plus the next cursor.", "service")
r("Service seam", "Notebooks", "As a Go caller I can read notebooks.",
  "ListNotebooks returns folders and their note counts.", "service")
r("Service seam", "Tags", "As a Go caller I can read tags.",
  "ListTags returns tags with their note counts.", "service")
r("Service seam", "Settings", "As a Go caller I can read and write settings.",
  "GetSetting/SetSetting round-trip through the service.", "service")


def main():
    fields = ["id", "area", "feature", "user_story", "expected", "status", "severity",
              "layer", "evidence", "error", "fix", "notes", "updated"]
    with open(OUT, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        w.writerows(ROWS)
    print(f"wrote {len(ROWS)} stories to {OUT}")


if __name__ == "__main__":
    main()
