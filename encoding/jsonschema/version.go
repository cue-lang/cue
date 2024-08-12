package jsonschema

import (
	"fmt"
	"net/url"
)

type schemaVersion int

const (
	versionDraft04 schemaVersion = iota
	versionDraft05
	versionDraft06
	versionDraft07
	version2019_09
	version2020_12
)

func parseSchemaVersion(sv string) (schemaVersion, error) {
	// TODO the URL "MUST" be normalized. Should we check that here?
	u, err := url.Parse(sv)
	if err != nil {
		return 0, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return 0, fmt.Errorf("invalid URL schema %q", u.Scheme)
	}
	if u.Host != "json-schema.org" {
		return 0, fmt.Errorf("unknown host")
	}
	switch u.Path {
	case "/draft-04/schema":
		return versionDraft04, nil
	case "/draft-05/schema":
		return versionDraft05, nil
	case "/draft-06/schema":
		return versionDraft06, nil
	case "/draft-07/schema":
		return versionDraft07, nil
	case "/draft/2019-09/schema":
		return version2019_09, nil
	case "/draft/2020-12/schema":
		return version2020_12, nil
	default:
		return 0, fmt.Errorf("$schema URI not recognized")
	}
}

func (v schemaVersion) String() string {
	switch v {
	case versionDraft04:
		return "http://json-schema.org/draft-04/schema#"
	case versionDraft05:
		return "http://json-schema.org/draft-05/schema#"
	case versionDraft06:
		return "http://json-schema.org/draft-06/schema#"
	case versionDraft07:
		return "http://json-schema.org/draft-07/schema#"
	case version2019_09:
		return "https://json-schema.org/draft/2019-09/schema"
	case version2020_12:
		return "https://json-schema.org/draft/2020-12/schema"
	}
	panic("unknown schema version")
}
