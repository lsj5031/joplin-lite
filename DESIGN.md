# Field Notes — Humanist

A quiet place to write: sage binding, warm paper, green ink, and a small folded-page mark.
Keep the workspace spacious and useful. Paper is expressed through color, typography, and
measure rather than texture, artificial folds, or decorative cards.

## Palette

| Role | Light | Dark |
| --- | --- | --- |
| Paper / editor | `#F9FAF5` | `#20271F` |
| Binding / navigation | `#E9EEE3` | `#1A2119` |
| Ink | `#344238` | `#E8ECDF` |
| Secondary ink | `#606F5A` | `#AFBAA5` |
| Hairline | `#DCE3D6` | `#364130` |
| Strong hairline | `#A8B69F` | `#67765C` |
| Accent / focus | `#566C4A` | `#C1D2AD` |

Selection uses a light sage tint and a clear edge. Red remains reserved for destructive
actions, conflicts, and errors; amber indicates warnings. Status always includes text.
Both the native shell and browser support the light and dark palettes.

## Typography

- **Optima** for the wordmark and headings: regular weight, restrained tracking, 24–40px.
- **Avenir Next** for reading, writing, navigation, and controls: reading text 17px with
  approximately 1.9 line height; controls 13–14px; secondary labels 11–12px.
- **Overpass Mono** for code and compact machine data such as timestamps and counts.
- Labels use sentence case. Avoid heavy, widely spaced uppercase navigation.

Optima and Avenir Next are requested from installed system fonts; Apple font files are never
redistributed. Native builds fall back to bundled Overpass when those families are absent.
Browser stacks also allow Avenir/Candara before bundled Overpass and system fallbacks.

## Workspace

Desktop has two panes by default: a 300px sage navigation/list column and a flexible paper
editor. Brand, search, and primary actions live above the list. Notes retain a clear selected
state and readable excerpts. The reading measure is capped at 700px with generous margins.

Notebooks and tags open on demand. Native desktop exposes a temporary inline notebook panel
so dragging notes remains possible; the browser uses a dialog. Narrow native windows and
mobile browser widths show the list or editor with an explicit Back action and a notebook
drawer. Management, search, and recovery controls must remain reachable at compact heights.

Use small 4–8px radii, tonal surfaces, and hairlines. Reserve shadows for transient overlays.
Avoid permanent ornamental chrome. Sync state remains real text and controls in a compact
footer; the former illustrative route diagram is removed.

## Mark and assets

The mark is a folded sheet in sage, formed from a few flat polygons. Keep it small beside the
Optima wordmark. Platform application icons place the same mark on paper for legibility.
Keep SVG, favicon, PNG, ICO, ICNS, and Windows executable resources consistent.

## Interaction and accessibility

Preserve keyboard navigation, visible focus, dialog dismissal/return, explicit destructive
confirmation, save errors, and no-sync operation. Typography and layout changes must not
change note persistence or hide controls. Test both writing and reading, empty/error states,
notebook management, compact windows, and browser light/dark themes using rendered screens.
No idle animation or new runtime dependency is needed for this visual language.
