package modregistry

import (
	"encoding/json"
	"fmt"
	"time"
)

// Metadata holds extra information that can be associated with
// a module. It is stored in the module's manifest inside
// the annotations field. All fields must JSON-encode to
// strings.
type Metadata struct {
	VCSType       string    `json:"org.cuelang.vcs-type"`
	VCSCommit     string    `json:"org.cuelang.vcs-commit"`
	VCSCommitTime time.Time `json:"org.cuelang.vcs-commit-time"`

	// GoPluginModuleVersion holds the version of the Go module
	// co-located with the CUE module that provides Go plugin code.
	GoPluginModuleVersion string `json:"org.cuelang.go-plugin-module-version,omitempty"`
}

func newMetadataFromAnnotations(annotations map[string]string) (*Metadata, error) {
	// TODO if this ever turned out to be a bottleneck we could
	// improve performance by avoiding the round-trip through JSON.
	raw, err := json.Marshal(annotations)
	if err != nil {
		// Should never happen.
		return nil, err
	}
	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Metadata) annotations() (map[string]string, error) {
	hasVCS := m.VCSType != "" || m.VCSCommit != "" || !m.VCSCommitTime.IsZero()
	if hasVCS {
		if m.VCSType == "" {
			return nil, fmt.Errorf("empty metadata value for field %q", "org.cuelang.vcs-type")
		}
		if m.VCSCommit == "" {
			return nil, fmt.Errorf("empty metadata value for field %q", "org.cuelang.vcs-commit")
		}
		if m.VCSCommitTime.IsZero() {
			return nil, fmt.Errorf("no commit time in metadata")
		}
	}
	// TODO if this ever turned out to be a bottleneck we could
	// improve performance by avoiding the round-trip through JSON.
	data, err := json.Marshal(m)
	if err != nil {
		// Should never happen.
		return nil, err
	}
	var annotations map[string]string
	if err := json.Unmarshal(data, &annotations); err != nil {
		// Should never happen.
		return nil, err
	}
	for field, val := range annotations {
		if val == "" {
			delete(annotations, field)
		}
	}
	// time.Time zero value marshals to a non-empty string;
	// remove it when VCS info isn't present.
	if !hasVCS {
		delete(annotations, "org.cuelang.vcs-commit-time")
	}
	if len(annotations) == 0 {
		return nil, fmt.Errorf("no metadata values set")
	}
	return annotations, nil
}
