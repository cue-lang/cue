module cuelang.org/go

go 1.25.0

require (
	cuelabs.dev/go/oci/ociregistry v0.0.0-20251212221603-3adeb8663819
	github.com/cockroachdb/apd/v3 v3.2.3
	github.com/coder/websocket v1.8.14
	github.com/emicklei/proto v1.14.3
	github.com/go-quicktest/qt v1.102.0
	github.com/google/go-cmp v0.7.0
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/google/uuid v1.6.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/pelletier/go-toml/v2 v2.3.0
	github.com/protocolbuffers/txtpbfmt v0.0.0-20260217160748-a481f6a22f94
	github.com/rogpeppe/go-internal v1.14.2-0.20260415112238-aa1b1e25579a
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/tetratelabs/wazero v1.11.0
	github.com/yuin/goldmark v1.8.2
	go.yaml.in/yaml/v3 v3.0.4
	golang.org/x/mod v0.34.0
	golang.org/x/net v0.52.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/sync v0.20.0
	golang.org/x/text v0.35.0
	golang.org/x/tools v0.43.0
	mvdan.cc/sh/v3 v3.13.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	golang.org/x/sys v0.43.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
)

replace github.com/rogpeppe/go-internal => /home/rogpeppe/src/go-internal

tool (
	cuelang.org/go/cmd/cue
	golang.org/x/tools/cmd/stringer
)
