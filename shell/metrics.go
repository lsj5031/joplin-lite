package main

import (
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// metricSpan identifies a measured UI or backend hot path in the desktop shell.
type metricSpan int

const (
	spanBoot metricSpan = iota
	spanUIFrame
	spanUIRailPane
	spanUINotesPane
	spanUIEditorPane
	spanUIWordCount
	spanUIMarkdownPreview
	spanUICommandPalette
	spanReloadNotes
	spanLoadMore
	spanShowNote
	spanMarkDirty
	spanFlushSave
	spanReloadNotebooks
	spanReloadTags
	spanLoadImage
	spanRefreshAfterSync
	spanSyncPass
	spanCount
)

type spanMeta struct {
	name      string
	threshold time.Duration
}

var spanTable = [spanCount]spanMeta{
	spanBoot:              {name: "op.boot", threshold: 100 * time.Millisecond},
	spanUIFrame:           {name: "ui.frame", threshold: 16 * time.Millisecond},
	spanUIRailPane:        {name: "ui.rail_pane", threshold: 8 * time.Millisecond},
	spanUINotesPane:       {name: "ui.notes_pane", threshold: 8 * time.Millisecond},
	spanUIEditorPane:      {name: "ui.editor_pane", threshold: 8 * time.Millisecond},
	spanUIWordCount:       {name: "ui.word_count", threshold: 2 * time.Millisecond},
	spanUIMarkdownPreview: {name: "ui.markdown_preview", threshold: 12 * time.Millisecond},
	spanUICommandPalette:  {name: "ui.command_palette", threshold: 8 * time.Millisecond},
	spanReloadNotes:       {name: "op.reload_notes", threshold: 16 * time.Millisecond},
	spanLoadMore:          {name: "op.load_more", threshold: 16 * time.Millisecond},
	spanShowNote:          {name: "op.show_note", threshold: 16 * time.Millisecond},
	spanMarkDirty:         {name: "op.mark_dirty", threshold: 4 * time.Millisecond},
	spanFlushSave:         {name: "op.flush_save", threshold: 16 * time.Millisecond},
	spanReloadNotebooks:   {name: "op.reload_notebooks", threshold: 16 * time.Millisecond},
	spanReloadTags:        {name: "op.reload_tags", threshold: 16 * time.Millisecond},
	spanLoadImage:         {name: "op.load_image", threshold: 25 * time.Millisecond},
	spanRefreshAfterSync:  {name: "op.refresh_after_sync", threshold: 25 * time.Millisecond},
	spanSyncPass:          {name: "sync.pass", threshold: 2 * time.Second},
}

const (
	metricSampleWindow = 256
	slowLogCooldown    = time.Second
)

type spanWindow struct {
	count       uint64
	slowCount   uint64
	totalNs     int64
	lastNs      int64
	maxNs       int64
	samples     [metricSampleWindow]int64
	sampleIdx   int
	sampleLen   int
	lastSlowLog time.Time
}

// spanSnapshot is a point-in-time latency summary for one measured span.
type spanSnapshot struct {
	Span      string
	Threshold time.Duration
	Count     uint64
	SlowCount uint64
	Last      time.Duration
	Avg       time.Duration
	P50       time.Duration
	P95       time.Duration
	Max       time.Duration
	Total     time.Duration
}

// uiMetrics collects rolling and cumulative timings across the native UI and
// synchronous store calls. Fast calls update fixed-size counters without heap
// allocation; calls that exceed their frame/operation budget emit rate-limited
// structured warnings to desktop.log and stderr.
type uiMetrics struct {
	mu      sync.Mutex
	spans   [spanCount]spanWindow
	enabled bool
	verbose bool
}

func newUIMetrics() *uiMetrics {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("JOPLIN_METRICS")))
	enabled := env == "1" || env == "true" || env == "on" || env == "debug" || env == "verbose" || env == "all"
	verbose := env == "verbose" || env == "all"
	return &uiMetrics{
		enabled: enabled,
		verbose: verbose,
	}
}

// observe records the elapsed time since start for span. Pass time.Now() at
// the call site (`defer a.metrics.observe(spanUIFrame, time.Now())`) so the
// start timestamp is captured on entry without allocating a closure.
func (m *uiMetrics) observe(span metricSpan, start time.Time) {
	if m == nil || span < 0 || span >= spanCount {
		return
	}
	m.recordDuration(span, time.Since(start))
}

// observeAttrs records the elapsed time since start for span and attaches
// contextual attributes when logging a slow-operation warning.
func (m *uiMetrics) observeAttrs(span metricSpan, start time.Time, attrs ...slog.Attr) {
	if m == nil || span < 0 || span >= spanCount {
		return
	}
	m.recordDuration(span, time.Since(start), attrs...)
}

func (m *uiMetrics) recordDuration(span metricSpan, d time.Duration, attrs ...slog.Attr) {
	if m == nil || span < 0 || span >= spanCount {
		return
	}
	ns := max(int64(0), d.Nanoseconds())
	meta := spanTable[span]
	slow := meta.threshold > 0 && d >= meta.threshold

	var (
		shouldLog bool
		count     uint64
		slowCount uint64
		maxNs     int64
	)

	now := time.Now()
	m.mu.Lock()
	w := &m.spans[span]
	w.count++
	w.totalNs += ns
	w.lastNs = ns
	if ns > w.maxNs {
		w.maxNs = ns
	}
	w.samples[w.sampleIdx] = ns
	w.sampleIdx = (w.sampleIdx + 1) % metricSampleWindow
	if w.sampleLen < metricSampleWindow {
		w.sampleLen++
	}
	if slow {
		w.slowCount++
		if m.verbose || w.lastSlowLog.IsZero() || now.Sub(w.lastSlowLog) >= slowLogCooldown {
			w.lastSlowLog = now
			shouldLog = true
		}
	}
	count = w.count
	slowCount = w.slowCount
	maxNs = w.maxNs
	m.mu.Unlock()

	if shouldLog {
		logArgs := make([]any, 0, 12+len(attrs)*2)
		logArgs = append(logArgs,
			"span", meta.name,
			"duration_ms", durationMillis(d),
			"threshold_ms", durationMillis(meta.threshold),
			"count", count,
			"slow_count", slowCount,
			"max_ms", durationMillis(time.Duration(maxNs)),
		)
		for _, a := range attrs {
			logArgs = append(logArgs, a.Key, a.Value.Any())
		}
		slog.Warn("ui slow operation", logArgs...)
	}
}

// snapshot returns latency summaries for all spans that have been recorded at
// least once, in canonical span order.
func (m *uiMetrics) snapshot() []spanSnapshot {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]spanSnapshot, 0, spanCount)
	var scratch [metricSampleWindow]int64
	for i := metricSpan(0); i < spanCount; i++ {
		w := &m.spans[i]
		if w.count == 0 {
			continue
		}
		n := w.sampleLen
		buf := scratch[:n]
		copy(buf, w.samples[:n])
		slices.Sort(buf)

		p50 := time.Duration(percentileSorted(buf, 0.50))
		p95 := time.Duration(percentileSorted(buf, 0.95))
		avg := time.Duration(w.totalNs / int64(w.count))

		out = append(out, spanSnapshot{
			Span:      spanTable[i].name,
			Threshold: spanTable[i].threshold,
			Count:     w.count,
			SlowCount: w.slowCount,
			Last:      time.Duration(w.lastNs),
			Avg:       avg,
			P50:       p50,
			P95:       p95,
			Max:       time.Duration(w.maxNs),
			Total:     time.Duration(w.totalNs),
		})
	}
	return out
}

// logSummary writes a structured breakdown of recorded spans to slog when
// JOPLIN_METRICS is enabled or when any span exceeded its slow threshold.
func (m *uiMetrics) logSummary(reason string) {
	if m == nil {
		return
	}
	snaps := m.snapshot()
	if len(snaps) == 0 {
		return
	}
	hasSlow := false
	for _, s := range snaps {
		if s.SlowCount > 0 {
			hasSlow = true
			break
		}
	}
	if !m.enabled && !hasSlow {
		return
	}
	for _, s := range snaps {
		if !m.enabled && s.SlowCount == 0 && s.Span != "ui.frame" {
			continue
		}
		slog.Info("ui metrics summary",
			"reason", reason,
			"span", s.Span,
			"count", s.Count,
			"slow_count", s.SlowCount,
			"avg_ms", durationMillis(s.Avg),
			"p50_ms", durationMillis(s.P50),
			"p95_ms", durationMillis(s.P95),
			"max_ms", durationMillis(s.Max),
			"total_ms", durationMillis(s.Total),
		)
	}
}

func percentileSorted(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func durationMillis(d time.Duration) float64 {
	// Round to microsecond precision expressed in milliseconds (e.g. 1.234ms).
	return float64(d.Microseconds()) / 1000.0
}
