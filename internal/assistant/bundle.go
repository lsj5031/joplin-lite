package assistant

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed bundle/agent.js
var embeddedAgentSource string

//go:embed bundle/manifest.json
var embeddedManifest []byte

type BundleInfo struct {
	APIVersion int               `json:"apiVersion"`
	Entry      string            `json:"entry"`
	SHA256     string            `json:"sha256"`
	Bytes      int               `json:"bytes"`
	Packages   map[string]string `json:"packages"`
}

// Bundle describes the exact embedded agent artifact for diagnostics and
// notices. The bundle is committed so a Go-only build needs no Node.
func Bundle() (BundleInfo, error) {
	var info BundleInfo
	if err := json.Unmarshal(embeddedManifest, &info); err != nil {
		return BundleInfo{}, fmt.Errorf("read embedded agent manifest: %w", err)
	}
	return info, nil
}
