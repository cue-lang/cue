#!/usr/bin/env bash

set -eu
shopt -s extglob

# revendorInternal.sh is a script for "vendoring" (internal) parts of the
# golang.org/x repos for use locally. It currently fixes on versions of
# x/tools, x/tools/gopls and x/telemetry. It takes a list of target packages,
# vendors their transitive dependencies, then copies the resulting set of
# packages under a local directory. Import paths are adjusted to the new
# location, go:generate directives are stripped out, *_test.go and testdata
# directories are removed.
#
# Whilst this script could be adapted to be run regularly, it's most likely useful
# as a one-shot wrapper (which sort of suggests the module versions, package listsj)

# Save location of root of repo and change there to start
SCRIPT_DIR="$( command cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"/
cd $SCRIPT_DIR

# golang.org/x/tools(/gopls) version
toolsVersion=v0.16.2-0.20231226175357-025ebe628124
goplsVersion=v0.0.0-20231226175357-025ebe628124
telemetryVersion=v0.0.0-20231114163143-69313e640400

# Establish a temporary module to collect and vendor
# our internal requirements
td=$(mktemp -d)
trap "rm -rf $td" EXIT
cd $td
cat <<EOD > go.mod
module example.com
EOD
cat <<EOD > deps.go
package deps

import (
	_ "golang.org/x/tools/gopls"
	_ "golang.org/x/tools/gopls/internal/test/integration"
)
EOD

# Add replace directives by hand because go mod edit
# misinterprets a directory containing an '@' as an
# indication of the version
go mod edit -require=golang.org/x/tools@$toolsVersion
go mod edit -require=golang.org/x/tools/gopls@$goplsVersion
go mod edit -require=golang.org/x/telemetry@$telemetryVersion
go mod tidy
go mod vendor

cd $SCRIPT_DIR

tools_regex='s+golang.org/x/tools/internal+cuelang.org/go/internal/golangorgx/tools+g'
gopls_regex='s+golang.org/x/tools/gopls/internal+cuelang.org/go/internal/golangorgx/gopls+g'
telemetry_regex='s+golang.org/x/telemetry+cuelang.org/go/internal/golangorgx/telemetry+g'

# Force-sync to original
rsync -a --delete --chmod=D0755,F0644 $td/vendor/golang.org/x/tools/internal/ ./tools
rsync -a --delete --chmod=D0755,F0644 $td/vendor/golang.org/x/tools/gopls/internal/ ./gopls
rsync -a --delete --chmod=D0755,F0644 $td/vendor/golang.org/x/telemetry/ ./telemetry

# Adjust imports
find ./ -name "*.go" -exec sed -i $tools_regex {} +
find ./ -name "*.go" -exec sed -i $gopls_regex {} +
find ./ -name "*.go" -exec sed -i $telemetry_regex {} +

# Strip go:generate directives
find ./ -name "*.go" -exec sed -i '/^\/\/go:generate/d' {} +

# Remove _test.go files and testdata directories
find ./ -name "*_test.go" -exec rm {} +
find ./ -type d -name testdata -exec rm -rf {} +

