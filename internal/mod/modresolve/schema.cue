// Copyright 2024 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// #file represents the registry configuration schema.
#file: {
	// moduleRegistries specifies a mapping from module path prefix
	// (excluding any version suffix) to the registry to be used for
	// all modules under that path.
	//
	// A prefix is considered to match if a non-zero number of
	// initial path elements (sequences of non-slash characters) in
	// a module path match the prefix.
	//
	// If there are multiple matching prefixes, the longest
	// is chosen.
	moduleRegistries: [#modulePath]: #registry

	// defaultRegistry specifies a fallback registry to be used if no
	// prefix from moduleRegistry matches.
	// If it's not present, a system default will be used.
	defaultRegistry?: #registry
}

#registry: {
	// host specifies the host name or host:port pair of the registry.
	// IPv6 host names should be enclosed in square brackets.
	host!: #hostname

	// insecure specifies whether an insecure connection should
	// be made to the host.
	insecure?: bool

	// repository specifies the repository in the registry for storing
	// modules. If pathEncoding is "path", this specifies the prefix
	// for all modules in the repository. For example, if repository
	// is foo/bar, module "x.example/y" will be stored at
	// "foo/bar/x.example/y".
	repository?: #repository

	// pathEncoding specifies how module versions map to repositories
	// within a registry.
	// Possible values are:
	// - "path": the repository is used as a prefix to the unencoded
	// module path. The version of the module is used as a tag.
	// - "hashAsPath": the hex-encoded SHA256 hash of the path
	// is used as a suffix to the above repository value. The version
	// of the module is used as a tag.
	// - "hashAsTag": the repository is used as is: the hex-encoded
	// SHA256 hash of the path followed by a hyphen and the version
	// is used as a tag.
	pathEncoding?: "path" | "hashAsRepo" | "hashAsTag"

	// prefixForTags specifies an arbitrary prefix that's added to all tags.
	// This can be used to disambiguate tags when there might be
	// some possibility of confusion with tags in use for other purposes.
	prefixForTags?: #tag

	// stripPrefix specifies that the pattern prefix should be
	// stripped from the module path before using as a repository
	// path. This only applies when pathEncoding is "path".
	stripPrefix?: bool
}

// TODO more specific schemas below
#modulePath: string
#hostname: string
#repository: string
#tag: string

// This aspect of #registry encodes the defaults used by the resolver
// parser. It's kept separate because it's technically bad practice to
// define regular fields as part of a schema, and by defining it this
// way, the pure schema can be read independently as such.
//
// TODO work out a nice way of doing this such that we don't have to
// mirror the fields in #file that mention #registry
#registry: {
	pathEncoding: *"path" | _
}
