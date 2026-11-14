# Field Notes — Humanist

A quiet place to write: sage binding, warm paper, green ink, and a small folded-ribbon J mark.
Keep the workspace spacious and useful. Paper is expressed through color, typography, and
measure rather than texture or artificial folds. Native editor and Ask cards float over
the theme wash; the browser retains a continuous paper reading surface.

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

- **Optima** for editorial headings: regular weight, restrained tracking. Native note
  titles start at 24 DIP. The browser uses 40 CSS px on desktop and 25px below 1024px.
- **Avenir Next** for reading and writing: native body text starts at 17 DIP with
  1.6 line height; controls 13–14 DIP; secondary labels 11–12 DIP. These are baseline
  sizes subject to OS accessibility text scaling, not fixed physical pixels.
- **Overpass Mono** for code and compact machine data such as timestamps and counts.
- Labels use sentence case. Avoid heavy, widely spaced uppercase navigation.

Optima and Avenir Next are requested from installed system fonts; Apple font files are never
redistributed. Native builds fall back to bundled Overpass when those families are absent.
The browser body stack allows Avenir before bundled Overpass; headings allow Candara
after Optima. Browser body text is 17 CSS px with 1.95 line height. These existing web
choices are explicit surface exceptions, not a reason for different Mac/Windows native
content typography. Native controls/chrome use the platform UI font (default theme
size 14 DIP on Windows, 13 DIP on macOS); the Windows header uses that font too.
Browser zoom and native OS scaling must preserve reachable controls and readable text.

## Workspace

### Windows

Windows keeps the README's Field Notes light/dark palettes: sage navigation,
warm paper, forest-green accents, and the bundled humanist reading families.
Controls and window chrome use the system UI font. On supported Windows 11
systems, Mica sits beneath the theme wash while the editor stays opaque for
ClearType text rendering.

Use one integrated 48 DIP toolbar, following the
[Kero chrome pattern](https://github.com/egoist/kero/blob/main/platform.go).
MyGO owns the caption buttons; reserve their reported title-bar insets, keep
drag space around interactive controls, and expose menus through Alt or F10
instead of a permanent menu row. Older Windows versions use an opaque Field Notes
background. The shared MyGO glass effects remain on search, toolbar controls,
hover states, selections, and transient surfaces on every platform.
The integrated Windows chrome changes the window structure, not the product theme.

### Shared workspace and platform exceptions

Keep the same palette roles, action hierarchy, humanist content families and reading
measure across native platforms. Native note rows are compact and title-only, with
updated time and a to-do control where applicable; the browser retains taller excerpt
rows. Selection must remain clear without relying on color alone.

Native writing and preview content share a 700 DIP maximum measure, shrinking with the
pane and responsive side padding. The browser uses 700 CSS px as its desktop content
measure with at least 48px side padding; rendered prose also has a 72ch maximum. Below
1024px it uses 24px side padding. A maximum is a ceiling, not a required width. Favor
space around the document over increasing line length in wide windows.

Use a 4px spacing rhythm: compact gaps 4–8, control/panel padding 12–16, and larger
document margins 24–48, adjusted for available width and text scaling. Keep type,
spacing and material decisions in `shell/design.go` and `web/src/styles/app.css`.

The native list begins at 300 DIP and can be resized; the browser desktop library
column is 300 CSS px. macOS puts search/actions in the list-local header and
reserves traffic-light space; its wide inset layout currently omits the brand group.
Windows has the global integrated header described above, with logo/name first,
then the notebook action. Both preserve the same primary
actions. Keep system menus, caption controls, Cmd/Ctrl conventions and native font
rasterization. Mica and macOS sidebar vibrancy are platform backdrops, not shared
shader glass. Native corner rendering can differ with the platform renderer.

Notebooks and tags open on demand. Native desktop exposes a temporary inline notebook panel
so dragging notes remains possible; the browser uses a dialog. Narrow native windows and
mobile browser widths show the list or editor with an explicit Back action and a notebook
drawer. Management, search, and recovery controls must remain reachable at compact heights.

### Material roles

Use small radii for controls, tonal surfaces and hairlines. Native floating editor/Ask
cards use the shared 11 DIP card radius, a fine border and soft ambient shadow to separate
them from the wash. Windows cards stay opaque for ClearType; macOS cards normally
use 92% light / 90% dark paper, becoming opaque with Reduce Transparency. This is
an intentional text-rendering exception.
Transient menus/popovers use the shared elevated surface treatment. Browser dialogs
use CSS surfaces/shadows; they do not use the MyGO shader or OS vibrancy.

The [MyGO Glass guide](https://github.com/egoist/mygo/blob/main/docs/plugins/glass.md)
assigns Regular glass to readable bars/buttons/panels and Clear to content that brings
its own contrast. Use glass for controls, selection and transient/navigation surfaces,
not document text. The current native rail uses Clear; contrast across theme washes
still needs visual review before changing its material. Extra app shadows are an
application treatment: the guide's glass rim itself is shadowless. Adding more glass
does not establish platform consistency. `Glass{Native: true}` samples a webview on
supported Macs; this native workspace has no webview and uses shader glass.

The [Native UI index](https://github.com/egoist/mygo/blob/main/docs/ui/README.md)
defines the native widget, focus, overlay and accessibility contracts. Consult the
actual pinned/patched dependency before adopting current-guide APIs. The app retains
MyGO v0.9.0, whose glass carries the Reduce Transparency behavior this design needs:
Reduce Transparency reaches UI and Painter, Regular and interactive glass becomes
opaque theme Surface (an interactive tint survives), Clear becomes opaque theme
Background, and Blur plus soft/hard ScrollEdge becomes opaque theme Background.
High Contrast strengthens the glass rim and adds an opaque theme Border to
reduced-transparency glass. App cards and the wash base become opaque too;
decorative glow can remain above the wash.

The pinned platform backends read macOS Reduce Transparency and Windows' transparency
setting; Linux does not populate this preference. The MyGO native-UI browser backend
reads its media query, but this React preview is a separate implementation. CPU pixel
tests cover all six materials in light/dark and changing backdrops; they do not verify
live OS preference toggles, Mica/vibrancy behavior or actual macOS rendering. Native
backdrop requests are unchanged. Current-guide Clarity/native-webview-glass APIs are
not available in this pin. See `shell/patches/README.md` for maintained patch details.
Status remains real text and controls in a compact footer. In the browser, that status
describes local saving/connection and Reload, rather than native remote-sync controls.

## Mark and assets

The mark is a folded-ribbon J monogram in sage, formed from a few flat polygons. Keep it small
beside the wordmark (editorial font where used, system UI font in the Windows header).
Platform application icons place the same mark on paper for legibility.
Keep SVG, favicon, PNG, ICO, ICNS, and Windows executable resources consistent.

## Interaction and accessibility

Preserve keyboard navigation, visible focus, dialog dismissal/return, explicit destructive
confirmation, save errors, and no-sync operation. Typography and layout changes must not
change note persistence or hide controls. Test both writing and reading, empty/error states,
notebook management, compact windows, and browser light/dark themes using rendered screens.
No idle animation or new runtime dependency is needed for this visual language.

Compare native platforms using the same commit, disposable notes, theme, wash, viewport
in DIPs and scale. Simulated macOS screenshots are presentation fixtures, not live Mac
evidence. Headless app-content images cannot prove Windows non-client geometry or OS
caption removal; retain separate hidden-HWND checks. Browser DOM checks should exercise
platform shortcut labels and modal focus, with rendered inspection for visual changes.
