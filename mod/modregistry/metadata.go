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
	// The "is-empty" checks don't work for time.Time
	// so check explicitly.
	if m.VCSCommitTime.IsZero() {
		return nil, fmt.Errorf("no commit time in metadata")
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
			return nil, fmt.Errorf("empty metadata value for field %q", field)
		}
	}
	return annotations, nil
}
