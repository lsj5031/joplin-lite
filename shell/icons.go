package main

import "github.com/egoist/mygo/ui"

// Embedded 16×16 vector icons for native chrome, navigation, and actions.
// Each SVG is parsed once at startup and rendered through ui.Icon so it
// inherits the active text color and scales cleanly at any DPI.
var (
	iconSidebar = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2.5" width="12" height="11" rx="2"/><line x1="6" y1="2.5" x2="6" y2="13.5"/></svg>`))

	iconPlus = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="3.25" x2="8" y2="12.75"/><line x1="3.25" y1="8" x2="12.75" y2="8"/></svg>`))

	iconSettings = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><line x1="2.5" y1="4.5" x2="13.5" y2="4.5"/><line x1="2.5" y1="8" x2="13.5" y2="8"/><line x1="2.5" y1="11.5" x2="13.5" y2="11.5"/><circle cx="5.5" cy="4.5" r="1.5" fill="currentColor"/><circle cx="10.5" cy="8" r="1.5" fill="currentColor"/><circle cx="6.5" cy="11.5" r="1.5" fill="currentColor"/></svg>`))

	iconSearch = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><circle cx="7" cy="7" r="4.25"/><line x1="10.2" y1="10.2" x2="13.5" y2="13.5"/></svg>`))

	iconClose = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><line x1="4.25" y1="4.25" x2="11.75" y2="11.75"/><line x1="11.75" y1="4.25" x2="4.25" y2="11.75"/></svg>`))

	iconMore = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><circle cx="3.5" cy="8" r="1.25"/><circle cx="8" cy="8" r="1.25"/><circle cx="12.5" cy="8" r="1.25"/></svg>`))

	iconChevronLeft = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="9.75 3.75 5.5 8 9.75 12.25"/></svg>`))

	iconChevronRight = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6.25 3.75 10.5 8 6.25 12.25"/></svg>`))

	iconSort = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3.5v9M5 3.5L2.75 5.75M5 3.5l2.25 2.25"/><path d="M11 12.5v-9M11 12.5l-2.25-2.25M11 12.5l2.25-2.25"/></svg>`))

	iconBookmark = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M4 2.75h8a1 1 0 0 1 1 1v9.75l-5-3-5 3V3.75a1 1 0 0 1 1-1z"/></svg>`))

	iconFolder = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M2 4.25a1.25 1.25 0 0 1 1.25-1.25h3.1l1.4 1.6h4.95A1.25 1.25 0 0 1 14 5.85v5.9A1.25 1.25 0 0 1 12.75 13H3.25A1.25 1.25 0 0 1 2 11.75z"/></svg>`))

	iconNotes = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="2.25" width="10" height="11.5" rx="1.5"/><line x1="5.5" y1="5.5" x2="10.5" y2="5.5"/><line x1="5.5" y1="8" x2="10.5" y2="8"/><line x1="5.5" y1="10.5" x2="8.5" y2="10.5"/></svg>`))

	iconTag = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 8.3V3.5a1 1 0 0 1 1-1h4.8l5.2 5.2a1.4 1.4 0 0 1 0 2l-3.8 3.8a1.4 1.4 0 0 1-2 0z"/><circle cx="5.25" cy="5.25" r="0.9" fill="currentColor"/></svg>`))

	iconPaperclip = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M13.2 7.4l-5.6 5.6a3.2 3.2 0 0 1-4.5-4.5l5.6-5.6a2.1 2.1 0 0 1 3 3l-5.5 5.5a1 1 0 0 1-1.5-1.5l5-5"/></svg>`))

	iconTrash = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><polyline points="2.75 4.25 13.25 4.25"/><path d="M5.75 4.25V3a1 1 0 0 1 1-1h2.5a1 1 0 0 1 1 1v1.25"/><path d="M4 4.25l.65 8.5a1.25 1.25 0 0 0 1.25 1.15h4.2a1.25 1.25 0 0 0 1.25-1.15l.65-8.5"/></svg>`))

	iconEdit = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M11.2 2.5a1.5 1.5 0 0 1 2.1 2.1L5.5 12.4l-3 .7.7-3z"/></svg>`))

	iconEye = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M1.5 8s2.4-4.25 6.5-4.25S14.5 8 14.5 8s-2.4 4.25-6.5 4.25S1.5 8 1.5 8z"/><circle cx="8" cy="8" r="1.85"/></svg>`))

	iconSync = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M13.25 5.5A5.5 5.5 0 0 0 3.4 4.75L2.25 6"/><polyline points="2.25 3 2.25 6 5.25 6"/><path d="M2.75 10.5a5.5 5.5 0 0 0 9.85.75l1.15-1.25"/><polyline points="13.75 13 13.75 10 10.75 10"/></svg>`))

	iconCheck = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><polyline points="3.25 8.25 6.5 11.5 12.75 4.75"/></svg>`))

	iconAlert = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M7.13 2.75a1 1 0 0 1 1.74 0l5.18 9a1 1 0 0 1-.87 1.5H2.82a1 1 0 0 1-.87-1.5z"/><line x1="8" y1="6.5" x2="8" y2="9.25"/><circle cx="8" cy="11.35" r="0.6" fill="currentColor"/></svg>`))
)
