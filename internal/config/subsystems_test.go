package config

import (
	"reflect"
	"testing"
)

// TestUnsupportedSubsystems pins the "never a silent no-op" contract: a setting
// that has no runtime implementation is reported so the app can warn instead of
// pretending it took effect. Covers OBS-01..OBS-04.
func TestUnsupportedSubsystems(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want []string
	}{
		{"defaults are supported", Config{}, nil},
		{"tracing", Config{Observability: ObservabilityConfig{Tracing: TracingConfig{Enabled: true}}},
			[]string{"observability.tracing"}},
		{"metrics", Config{Observability: ObservabilityConfig{Metrics: MetricsConfig{Enabled: true}}},
			[]string{"observability.metrics"}},
		{"rag", Config{RAG: RAGConfig{Enabled: true}}, []string{"rag"}},
		{"mcp tool filter", Config{MCPEnabledTools: "create_note,search_notes"},
			[]string{"mcp.enabled_tools"}},
		{"mcp wildcard is supported", Config{MCPEnabledTools: "*"}, nil},
		{"all", Config{
			Observability:   ObservabilityConfig{Tracing: TracingConfig{Enabled: true}, Metrics: MetricsConfig{Enabled: true}},
			RAG:             RAGConfig{Enabled: true},
			MCPEnabledTools: "create_note",
		}, []string{"observability.tracing", "observability.metrics", "rag", "mcp.enabled_tools"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.UnsupportedSubsystems(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("UnsupportedSubsystems() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestObservabilityDefaultsDisabled guards against regressing to the old
// behaviour where tracing/metrics defaulted on while doing nothing.
func TestObservabilityDefaultsDisabled(t *testing.T) {
	o := DefaultObservability()
	if o.Tracing.Enabled || o.Metrics.Enabled {
		t.Fatalf("observability must default to disabled: %+v", o)
	}
}
