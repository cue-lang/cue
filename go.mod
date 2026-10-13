module cuelang.org/go

go 1.26.0

require (
	cuelabs.dev/go/oci/ociregistry v0.0.0-20261008151123-94f4aa6e0087
	github.com/bufbuild/protocompile v0.14.2-0.20260824170637-a01cc8856977
	github.com/cockroachdb/apd/v3 v3.2.3
	github.com/coder/websocket v1.8.15
	github.com/emicklei/proto v1.14.3
	github.com/go-quicktest/qt v1.102.0
	github.com/goccy/go-yaml v1.19.2
	github.com/google/go-cmp v0.7.0
	github.com/google/uuid v1.6.0
	github.com/opencontainers/go-digest v1.0.1-0.20260914153615-3650f26df2a1
	github.com/opencontainers/image-spec v1.1.1
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/protocolbuffers/txtpbfmt v0.0.0-20261013103202-7c8a61a7a736
	github.com/rogpeppe/go-internal v1.16.1-0.20261002162744-ce71b23481ca
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/tetratelabs/wazero v1.12.0
	github.com/yuin/goldmark/v2 v2.1.6
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/mod v0.42.0
	golang.org/x/net v0.61.0
	golang.org/x/oauth2 v0.37.0
	golang.org/x/sync v0.24.0
	golang.org/x/text v0.43.0
	golang.org/x/tools v0.52.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/petermattis/goid v0.0.0-20260716134002-a9b348f0a2b9 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

tool (
	cuelang.org/go/cmd/cue
	golang.org/x/tools/cmd/stringer
)
