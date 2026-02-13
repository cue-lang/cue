module: "github.com/cue-lang/tmp/internal/ci"
language: {
	version: "v0.16.0"
}
source: {
	kind: "self"
}
deps: {
	"cue.dev/x/githubactions@v0": {
		v:       "v0.3.0"
		default: true
	}
}
