---
name: joplin-lite
description: A lightweight Joplin peer — notes, E2EE, sync, and the web clipper, drawn like transit service signage.
colors:
  paper: "#F6F4EE"
  paper-sunken: "#EDE8DD"
  paper-raised: "#FBFAF6"
  ink-navy: "#16283C"
  ink-soft: "#4A5A6E"
  hairline: "#D9D2C4"
  hairline-strong: "#BEB5A3"
  navy-deep: "#0F1D2D"
  navy-line: "#33455C"
  navy-text: "#A8B4C4"
  route-blue: "#1D5FA8"
  route-blue-soft: "#9CC1E8"
  moss: "#276B43"
  moss-bright: "#3E9B63"
  signal-red: "#C8322B"
  signal-red-bright: "#E05A52"
  amber-ink: "#8A5A12"
  amber-bright: "#C9821B"
  violet: "#6B4E9B"
typography:
  display:
    fontFamily: "Overpass, ui-sans-serif, system-ui, sans-serif"
    fontSize: "24px"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.015em"
  title:
    fontFamily: "Overpass, ui-sans-serif, system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "-0.01em"
  body:
    fontFamily: "Overpass, ui-sans-serif, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.65
    letterSpacing: "normal"
  label:
    fontFamily: "Overpass, ui-sans-serif, system-ui, sans-serif"
    fontSize: "11px"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "0.08em"
  mono:
    fontFamily: "Overpass Mono, ui-monospace, monospace"
    fontSize: "11px"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "tnum"
rounded:
  sm: "4px"
  md: "6px"
  lg: "8px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
components:
  button-primary:
    backgroundColor: "{colors.route-blue}"
    textColor: "{colors.paper}"
    rounded: "{rounded.md}"
    padding: "6px 10px"
  button-primary-hover:
    backgroundColor: "{colors.route-blue}"
    textColor: "{colors.paper}"
  button-secondary:
    backgroundColor: "{colors.paper-raised}"
    textColor: "{colors.ink-navy}"
    rounded: "{rounded.md}"
    padding: "6px 10px"
  chip-tag:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink-soft}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
  input-search:
    backgroundColor: "{colors.navy-deep}"
    textColor: "{colors.paper}"
    rounded: "{rounded.md}"
    height: "32px"
  nav-row:
    backgroundColor: "{colors.paper-sunken}"
    textColor: "{colors.ink-navy}"
    rounded: "{rounded.md}"
    height: "32px"
---

# Design System: joplin-lite

## Overview

**Creative North Star: "The Network Signage"**

joplin-lite is a working notes application drawn like a transit authority's service board: the
library's sync network is a route line, every state is service status, and the work surfaces
stay as standard and quiet as the platform allows. The world contributes type, palette,
density, and exactly one signature move; it never contributes the layout, the navigation
model, or the controls. A user who knows any notes app should know where everything is before
they notice the world.

Density is timetable density: hairline-ruled rows, tight vertical rhythm, tabular figures for
anything a machine produced. The chrome is bookended in ink navy (top bar, status strip) over a
map-paper ground, so the eye lands on the work between two calm bars. State color does real
work and nothing else: green means synced, blue means moving, red means conflict, amber means
waiting or locked, violet means selected.

Rejected with evidence: the category-default grey three-pane app with a lone blue accent, and
its dark-neon "hacker vault" opposite. No textural nostalgia, no ornament — the transit world
is present as signage discipline, not as a poster.

**Key Characteristics:**
- Ink-navy chrome bookending a warm map-paper ground (#F6F4EE).
- Overpass / Overpass Mono (Highway Gothic lineage), self-hosted.
- Transit line colors as a strict state vocabulary.
- Hairline-ruled, timetable-dense lists with tabular figures.
- One signature: the sync line — a Beck-grammar route diagram in the status strip.

## Colors

A committed shell palette on a tinted paper ground; line colors are a service vocabulary, not
decoration.

### Primary
- **Route Blue** (#1D5FA8): primary actions (New note), focus rings, active syncing. The only
  saturated color allowed on controls at rest.
- **Route Blue Soft** (#9CC1E8): the syncing line on navy chrome.

### Neutral
- **Map Paper** (#F6F4EE): the ground everything sits on.
- **Paper Sunken** (#EDE8DD): sidebar rail, hover fills, code chips.
- **Paper Raised** (#FBFAF6): the editor surface, one step brighter than the list.
- **Ink Navy** (#16283C): body text and the chrome bars (top bar, status strip).
- **Ink Soft** (#4A5A6E): secondary text (meta, counts, timestamps' container).
- **Hairline** (#D9D2C4) / **Hairline Strong** (#BEB5A3): row rules, borders, control strokes.
- **Navy Deep / Navy Line / Navy Text** (#0F1D2D / #33455C / #A8B4C4): chrome interiors, chrome
  borders, chrome secondary text.

### State (the service vocabulary)
- **Moss** (#276B43) / **Moss Bright** (#3E9B63): synced. Text-safe dark on paper, bright on navy.
- **Signal Red** (#C8322B) / **Signal Red Bright** (#E05A52): conflict and disruption.
- **Amber Ink** (#8A5A12) / **Amber Bright** (#C9821B): degraded, locked, waiting.
- **Violet** (#6B4E9B): selection — rows, active tab, caret, text selection.

### Named Rules
**The Service Color Rule.** Line colors carry state and only state. An idle control is never
colored like an alert; a state color never decorates a heading, a border, or an icon that
isn't reporting that state.

**The Two-Value Rule.** Every state color exists in a paper-safe and a navy-safe value (e.g.
moss #276B43 / moss-bright #3E9B63). Never place the paper value on chrome or vice versa.

## Typography

**Display/Body Font:** Overpass (fallback: ui-sans-serif, system-ui) — Highway Gothic lineage,
signage voice.
**Mono Font:** Overpass Mono (fallback: ui-monospace) — data and only data.

**Character:** a signage grotesque with slight ink-trap warmth for everything human, and a
tabular mono for everything machine-made. The pairing reads like a well-set timetable.

### Hierarchy
- **Display** (700, 24px, 1.2, -0.015em): note titles in the rendered preview.
- **Title** (600, 20px, 1.3, -0.01em): the note title in the editor header.
- **Body** (400, 15px, 1.65): rendered note content, max measure 72ch.
- **UI** (400–600, 13–14px): list rows, nav, controls.
- **Label** (600, 11px, +0.08em, uppercase): sidebar section labels and panel headings only.
- **Mono** (400/500, 10–13px, tabular figures): timestamps, counts, IDs, station labels,
  markdown source.

### Named Rules
**The Tabular Rule.** Anything a machine produced — times, counts, sizes, IDs — is set in
Overpass Mono with tabular figures. Mono is never used to signal "technical" on prose.

## Layout

Three-pane workspace grid below a full-width top bar and above a full-width status strip:
`248px | 360px | 1fr` at ≥1024px (rail / note list / editor). Spacing rhythm is a 4px base
(4/8/12/16/24); list rows sit at ~40px with 1px hairline rules; panes pad 20–24px; more space
above a heading than below it.

Below 1024px the shell collapses: the rail becomes a slide-over behind a "Notebooks" button,
list and editor swap as single views with a back affordance, the top bar collapses its button
labels to icons, and the status strip stacks (diagram first, status text beneath) so the sync
line stays full width. Nothing scrolls horizontally except the note list and the sync line
itself at extreme narrowness.

## Elevation & Depth

Flat by default: depth is hairline borders, not shadows. Elevation is declared once per
surface — a resting surface carries a border **or** nothing. Exactly one shadow vocabulary
entry exists, for transient overlays only (settings popover). No cards float over the
workspace.

### Shadow Vocabulary
- **Popover** (`box-shadow: 0 6px 18px rgba(15,29,45,0.22)`): transient overlays above the
  chrome only.

### Named Rules
**The Hairline Rule.** Resting surfaces separate with 1px hairlines and tonal steps
(paper → sunken → raised). If a new component wants a shadow, it wants a border.

## Shapes

Small radii, signage-flat: 6px on controls (buttons, inputs, rows' hover fill), 4px on kbd
hints and code chips, 8px on the popover, full pills only for tag chips. Borders are 1px;
selection edges are 1px violet. Square, calm silhouettes — no bevels, no glass, no gradients.

## Components

Standard web controls throughout, rebuilt in the signage vocabulary.

### Buttons
- **Shape:** 6px radius, 6px/10px padding, 12.5px semibold.
- **Primary:** Route Blue fill, paper text (New note, Unlock vault). Hover: opacity 0.9,
  100ms.
- **Secondary:** paper/transparent fill, hairline (paper) or navy-line (chrome) border, ink or
  paper text (Sync now, settings, Mark complete).
- **State buttons:** same shape with state-colored border + 10% tint fill when active.

### Chips
- **Tag chips:** pill radius, hairline-strong border, 11px ink-soft text.
- **State chips (chrome):** 6px radius, state-colored border and text (vault locked/unlocked).

### Inputs / Fields
- **Search:** 32px height, navy-deep fill on chrome, 6px radius, Route Blue focus border,
  mono kbd hint right-aligned; themed caret and search icon.
- **Editor title/body:** borderless fields; body is Overpass Mono 13px with violet caret.

### Navigation
- Sidebar rows: 32px, 6px radius, counts in mono right-aligned; hover = sunken fill; active =
  violet 10% fill with 1px violet edge and semibold text. Tree indent with hairline spine.

### List Rows (note list)
- ~40px rows, 1px hairline rule between, flag column (todo / conflict icons), title + meta
  stack, updated timestamp right in mono. Active row: violet tint + 1px violet edge.

### Signature: The Sync Line
- A Beck-grammar route diagram (3px strokes, 90°/45° bends only, ringed station dots) of the
  library's network: joplin-lite → Dropbox (target 7) → Joplin desktop, with a 45° branch to
  Joplin mobile and a stub to the clipper (:41184). Segments and stations take service colors;
  syncing runs a paper dash along the active segment; a conflict marks the target station with
  a red disruption cross. It lives full-width in the status strip and nowhere else.

## Do's and Don'ts

### Do:
- **Do** keep every state legible in the sync line before any copy explains it.
- **Do** set all machine-produced data in Overpass Mono with tabular figures (times, counts,
  IDs).
- **Do** use the state vocabulary exactly: moss = synced, route = moving/primary,
  signal = conflict, amber = waiting/locked, violet = selected.
- **Do** collapse to icon-labeled chrome and stacked footer below 1024px rather than letting
  the shell overflow.

### Don't:
- **Don't** spend a state color on decoration, or use the paper-safe value on navy chrome
  (see the Two-Value Rule).
- **Don't** float resting surfaces with shadows; hairlines and tonal steps are the depth
  system.
- **Don't** costume controls in the transit world — navigation, buttons, inputs, and tabs stay
  standard web controls.
- **Don't** place the sync line anywhere but the status strip, and never animate it when no
  sync is running.
