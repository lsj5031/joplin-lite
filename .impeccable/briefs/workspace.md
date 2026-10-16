# Surface brief — joplin-lite workspace

Scope: the primary application screen (the whole product shell, first build). Mode: Operate.
Build path: code-led (Operate surfaces are code-led regardless of project defaults).

Audience: the project owner — a developer who lives in one Joplin library shared with
desktop/mobile clients over Dropbox with E2EE. Job: find, read, edit, and clip notes fast,
and know at a glance whether the library is synced and the vault is unlocked. Frequency:
many times daily, keyboard-first, long sessions in even light. Task states that must read
clearly: populated library, selected note editing, search hit / search empty, conflict note
present, syncing, sync error/target locked, vault locked. Constraints: desktop webview window
(min ~1100×700, must survive narrow/mobile widths), synthetic mock data in this phase labeled
synthetic, standard web controls only.

Chosen direction: grounded candidate 6 — "Network Signage" (transit map & waybill world).
Memorable moment: the sync line — a Beck-grammar route diagram of the library's network with
service colors, so "is my library consistent?" is answered before the eye reaches the notes.

Unresolved decisions: webview hosting of the Start build (M4); whether the sync line ever
becomes interactive (no for v1).

## Direction contract

THESIS: A working three-pane Joplin client whose spine is the sync network itself — every
state (synced, syncing, conflict, locked) reads like transit service status on one line. It
refuses the category default: grey three-pane app with a lone blue accent, and its dark-neon
"hacker vault" opposite.

OWN-WORLD: Map-paper ground #F6F4EE; ink-navy chrome #16283C; transit line colors doing real
jobs — line blue #1D5FA8 (primary/syncing), moss #2E7D4F (synced), signal red #C8322B
(conflict), amber #C9821B (degraded/locked), violet #6B4E9B (selection). Overpass (Highway
Gothic lineage) for UI/display, Overpass Mono for times, IDs, sizes. Hairline-ruled timetable
density, tabular figures, signage-flat controls at 6px radius.

STORY: The visitor opens the app and knows what is synced before reading a word; notes are
read and written in a familiar shell; a conflict shows as a "disruption" that keeps both
versions and says how to resolve it.

FIRST VIEWPORT: Standard shell — top bar (mark, search field, vault state, sync button), left
rail (notebooks tree + tags), middle dense note list (title, notebook, updated_time mono,
flags), right markdown editor with preview toggle; footer status strip carries the sync line
at full width. No hero, no cards-as-page-structure.

FORM: Transit map & waybill ("Network Signage"), position 6 of my ordered grounded list,
SEED REFERENCE seed:45ef9038aa784ccb.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
