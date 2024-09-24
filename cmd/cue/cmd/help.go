// Copyright 2020 CUE Authors
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

package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/mod/modresolve"
	"cuelang.org/go/mod/modconfig"
)

// TODO: intersperse the examples at the end of the texts in the
// body of text to make things more concrete for the user early on?
// The current approach works will if users just print the text without
// "more" or "less", in which case the examples show more prominently.
// The user can then scroll up to get a more in-depth explanation. But is
// this how users use it?

// newHelpCmd is largely borrowed from cobra,
// but knows how to load custom commands in `cue help cmd`.
func newHelpCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "help [command]",
		Short:  "show help text for a command or topic",
		Hidden: true, // not shown as a runnable command in `cue help`
		Run: func(_ *cobra.Command, args []string) {
			findCmd := func() (*cobra.Command, bool) {
				cmd, rest, err := c.Root().Find(args)
				found := cmd != nil && err == nil && len(rest) == 0
				return cmd, found
			}
			cmd, found := findCmd()
			// Treat `cue help help` as an unknown help topic; just use `cue help`.
			if found && cmd.Name() == "help" {
				cmd, found = nil, false
			}
			isCmd := len(args) > 0 && args[0] == "cmd"
			if isCmd {
				// args is one of:
				//
				//	["cmd"]
				//	["cmd", "mycmd"]
				//	["cmd", "mycmd", "./mypkg"]
				//
				// In the third case, we want to load ["./mypkg"]
				// and we want to look up the help for ["cmd", "mycmd"].
				var pkgArgs []string
				if len(args) > 2 {
					pkgArgs = args[2:]
					args = args[:2]
				}

				tools, err := buildTools(c, pkgArgs)
				if err == nil {
					addCustomCommands(c, cmd, commandSection, tools)
					// For the sake of `cue help cmd mycmd`, find the command again.
					cmd, found = findCmd()
				}
			}
			if !found {
				if isCmd {
					// Note that for args ["cmd", "mycmd", "./mypkg"] we only want "mycmd".
					fmt.Fprintf(c.Stderr(), "Unknown cmd command: %s\n", args[1])
				} else {
					fmt.Fprintf(c.Stderr(), "Unknown help topic: %s\n", strings.Join(args, " "))
				}
				if cmd == nil {
					cobra.CheckErr(c.Root().Usage())
				} else {
					cobra.CheckErr(cmd.Usage())
				}
			} else {
				cobra.CheckErr(cmd.Help())
			}
		},
	}
	return cmd
}

// We use a custom Cobra help text template for the sake of being a bit more
// in control of its formatting, as well as the contents of the root `cue help`.
//
// TODO(mvdan): the text below should not jump straight into `cue cmd`,
// and instead give a high level overview of CUE and its commands.
var helpTemplate = `
{{- if not .HasParent}}{{/* Special template for the root help. */ -}}
cue evaluates CUE files, an extension of JSON, and sends them
to user-defined commands for processing.

Commands are defined in CUE as follows:

	import "tool/exec"
	command: deploy: {
		exec.Run
		cmd:   "kubectl"
		args:  ["-f", "deploy"]
		in:    json.Encode(userValue) // encode the emitted configuration.
	}

cue can also combine the results of http or grpc request with the input
configuration for further processing. For more information on defining commands
run 'cue help cmd' or go to cuelang.org/pkg/cmd.

For more information on writing CUE configuration files see cuelang.org.

Usage:
  {{.CommandPath}} [command]

Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

Use "{{.CommandPath}} help [command]" for more information about a command.

{{/* We want to show additional help topics from the root command's help text
     even though they are only reachable via 'cue help'. */ -}}
Additional help topics:{{range .Commands}}{{if eq .Name "help"}}{{range .Commands}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}
{{else}}{{/* Subcommands use a fairly standard template. */ -}}
{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}
{{- end -}}
`

var helpTopics = []*cobra.Command{
	commandsHelp,
	embedHelp,
	environmentHelp,
	filetypeHelp,
	flagsHelp,
	injectHelp,
	inputsHelp,
	modulesHelp,
	registryConfigHelp,
}

var inputsHelp = &cobra.Command{
	Use:   "inputs",
	Short: "package list, patterns, and files",
	Long: `Many commands apply to a set of inputs:

cue <command> [inputs]

The list [inputs] may specify CUE packages, CUE files, non-CUE
files or some combinations of those. An empty list specifies
the package in the current directory, provided there is a single
named package in this directory.

CUE packages are specified as an import path. An import path
that begins with a "." or ".." element is a relative import path,
is interpreted as a file system path, and denotes the
package instance in that directory.

Otherwise, the import path P is absolute and denotes a package that
may be external, usually found in an external registry. It may also
refer to a package in cue.mod/{pkg|gen|usr}/P - this is legacy
behavior that might be removed at a later date.

An absolute import path is of the form P or P@vN where vN is the major
version of the module containing the package. An import path is mapped
to a registry location by consulting cue.mod/module.cue and the
registry configuration (see "cue help modules" and "cue help
registryconfig" for more details).

An import may contain a ":name" suffix to indicate a specific
package to load within a given path. It is necessary to specify this
when there is more than one package at the path.

For example, the following import path specifies a package
named "other" in some external module with major version v1.

	foo.example/bar/baz@v1:other

A local import path may contain one or more "..." to match any
subdirectory: pkg/... matches all packages below pkg, including pkg
itself, while foo/.../bar matches all directories named bar within
foo. In all cases, directories containing cue.mod directories are
excluded from the result. "..." is not currently supported in external
import paths.

Directory and file names that begin with "." or "_" are ignored,
unless explicitly listed as inputs. File with names ending "_tool.cue"
are ignored unless running "cue cmd" and they are in packages
explicitly mentioned on the command line. Files with names ending
"_test.cue" are ignored for the time being; they are reserved for
future testing functionality.

A package may also be specified as a list of .cue files.
The special symbol '-' denotes stdin or stdout and defaults to
the cue file type for stdin. For stdout, the default depends on
the cue command. A .cue file package may not be combined with
regular packages.

Non-cue files are interpreted based on their file extension or,
if present, an explicit file qualifier (see the "filetypes"
help topic). By default, all recognized files are unified at
their root value. See the "filetypes" and "flags" help topics
on how to treat each file individually or how to combine them
differently.

If a data file has multiple values, such as allowed with JSON
Lines or YAML, each value is interpreted as a separate file.

If the --schema/-d is specified, data files are not merged, and
are compared against the specified schema within a package or
non-data file. For OpenAPI, the -d flag specifies a schema name.
For JSON Schema the -d flag specifies a schema defined in
"definitions". In all other cases, the -d flag is a CUE
expression that is evaluated within the package.

Examples (also see also "flags" and "filetypes" help topics):

# Show the definition of each package named foo for each
# directory dir under path.
$ cue def ./path/.../dir:foo

# Unify each document in foo.yaml with the value Foo in pkg.
$ cue export ./pkg -d Foo foo.yaml

# Unify data.json with schema.json.
$ cue export data.json schema: schema.json
`,
}

var embedHelp = &cobra.Command{
	Use:   "embed",
	Short: "file embedding",
	Long: `
The @embed attribute can be used to load files as part of CUE evaluation.
Embedding must be enabled by a file-level @extern(embed) attribute.

Embed a single file as follows, which must exist when evaluating:

	@extern(embed)

	package foo

	a: _ @embed(file=foo.json)

Embed many files of the same type with a glob pattern,
which inserts one field per file matched:

	b: _ @embed(glob=images/*.*, type=binary)
	b: [string]: bytes

Note that "**" glob patterns are not supported at this time.

If the file extension in "file" or "glob" does not imply a file type,
it must be specified with the "type" encoding as shown above.
See the "filetypes" help topic for more.

Files containing multiple documents cannot be embedded directly.
For NDJSON or multi-document YAML files, embed as type=text
and use APIs like yaml.Extract to decode as a list.

Note that embedding cue files is not supported at this time.

Note: support for embed is experimental, and is enabled via
CUE_EXPERIMENT=embed.

For more details and discussion, see the proposal linked from
https://cuelang.org/discussion/3264.
`[1:],
}

var environmentHelp = &cobra.Command{
	Use:   "environment",
	Short: "environment variables",
	Long: `
The cue command consults environment variables for configuration.
If an environment variable is unset or empty, sensible default setting is used.

	CUE_CACHE_DIR
		A directory structure to hold files which are reused between operations.
		It defaults to a directory for user-specific temporary data, such as:

		- "$XDG_CACHE_HOME/cue" or "$HOME/.cache/cue" on Linux
		- "$HOME/Library/Caches/cue" on MacOS
		- "%LocalAppData%/cue" on Windows

		Its contents are generally read-only and include:

		- mod/download for modules fetched from registries
		- mod/extract for extracted module archives

	CUE_CONFIG_DIR
		A directory to hold configuration and long-lived state files.
		It defaults to a directory for user-specific persistent data, such as:

		- "$XDG_CONFIG_HOME/cue" or "$HOME/.config/cue" on Linux
		- "$HOME/Library/Application Support/cue" on MacOS
		- "%AppData%/cue" on Windows

	CUE_REGISTRY
		The configuration to use when downloading and publishing modules.
		See "cue help registryconfig" for details.

	CUE_EXPERIMENT
		Comma-separated list of experiment flags to enable or disable:

		evalv3
			Enable the new CUE evaluator, addressing performance issues
			and bringing a better disjunction algorithm.
		embed
			Enable support for embedded data files as described in
			https://cuelang.org/discussion/3264.

	CUE_DEBUG
		Comma-separated list of debug flags to enable or disable, such as:

		http
			Log a JSON message per HTTP request and response made
			when interacting with module registries.

CUE_EXPERIMENT and CUE_DEBUG are comma-separated lists of key-value strings,
where the value is a boolean "true" or "1" if omitted. For example:

	CUE_EXPERIMENT=toenable,todisable=0
`[1:],
}

var modulesHelp = &cobra.Command{
	Use:   "modules",
	Short: "module support",
	Long: `Modules are how CUE publishes packages and manages dependencies.

A module is a collection of packages that are released, versioned, and
distributed together. Modules are downloaded from OCI-compliant
artifact registries.

The Central Registry for CUE modules (` + modconfig.DefaultRegistry + `) is
used as the source for external modules by default.

For a detailed reference on modules:

	https://cuelang.org/docs/reference/modules/

For information on commands that interact with modules:

    cue help mod

For tutorials on how to use the Central Registry, see:

	https://cuelang.org/docs/tutorial/working-with-the-central-registry/
	https://cuelang.org/docs/tutorial/publishing-modules-to-the-central-registry/

For a tutorial on how to work with a custom OCI registry for CUE modules:

	https://cuelang.org/docs/tutorial/working-with-a-custom-module-registry/

For information on how to specify the CUE registry:

	cue help registryconfig
`,
}

var registryConfigHelp = &cobra.Command{
	Use:   "registryconfig",
	Short: "module registry configuration",
	Long: `The registry configuration determines how CUE maps modules to their locations in
OCI registries.

Given a particular registry configuration, it's possible to work what registry a
given module is stored in, and what repository within that registry, without
making any network calls.

If no explicit registry configuration is present, a default Central Registry
(` + modconfig.DefaultRegistry + `) is used for all modules.

The simplest way of specifying a registry configuration is to set $CUE_REGISTRY
to the hostname of that registry.

Examples:

	CUE_REGISTRY=myregistry.example
	CUE_REGISTRY=localhost:5000
	CUE_REGISTRY='[::1]:5000'

The special name "none" can be used to indicate that no registry
should be used.

If a path is present too, all modules will be stored under that path.

For example:

	CUE_REGISTRY=localhost:5000/all/modules/will/be/stored/here

By default for hosts other than localhost, a secure (HTTPS) connection will be
used. This default can be set by appending "+insecure" or "+secure".

For example:

	CUE_REGISTRY=100.98.141.117:5000+insecure
	CUE_REGISTRY=localhost:5000/modules+secure

When specified in this way, the registry is used to fetch all modules.

To use a specific registry for only certain subsets of modules, a prefix can be
specified. This constrains a registry to be used only for modules that match
that prefix. If there are multiple registries with a prefix, the longest
matching prefix wins. When matching a prefix, only complete path elements
(non-slash characters) are considered. It's an error if there are two identical
prefixes in the same registry configuration.

For example:

	CUE_REGISTRY='foo.example/bar=localhost:5000,myregistry.example'

In the above example, modules with the prefix "foo.example/bar", such as
"foo.example/bar/somemodule" (but not "foo.example/barry"), will be fetched from
the registry at localhost:5000. All other modules will be fetched from
"myregistry.example".

Note that the syntax above implies that the ordering of the elements in
CUE_REGISTRY isn't important because the resolution algorithm is
order-independent.

To specify that no registry should be used for a given module prefix,
the special name "none" can be used.

For example:

	CUE_REGISTRY='foo.example/bar=none,myregistry.example'

In the above example, any attempt to fetch a module under
"foo.example/bar" will result in a failure. Note that this will not
take effect if the module is already present in the on-disk cache,
which is consulted before looking at CUE_REGISTRY.


Customizing Name Resolution

Some registries have restrictions on what repositories can be used. For example,
a registry implementation might restrict the depth of the available repository
paths, or might even provide access to a single repository only. In order to
accommodate these kinds of limitation, a registry configuration can also be
specified in CUE syntax, usually as a configuration file.

To do this, set CUE_REGISTRY to "file:" followed by the path to the
configuration file. For example:

	CUE_REGISTRY=file:/home/alice/.config/cue/registry.cue

To remove ambiguity, the simpler syntax described earlier can be explicitly
requested with the "simple:" prefix. For example:

	CUE_REGISTRY=simple:myregistry.example

If it is preferable not to create a file on disk for the configuration, the
"inline:" prefix can be used:

	CUE_REGISTRY='inline:defaultRegistry: registry: "myregistry.example"'

The configuration file syntax is described by the following CUE #file schema
definition:

` + indent(modresolve.RegistryConfigSchema()),
}

var flagsHelp = &cobra.Command{
	Use:   "flags",
	Short: "common flags for composing packages",
	Long: `Non-CUE files are merged at their roots by default.
The can be combined differently or treated as different files
by using a combination of the following flags.


Individual files

To treat non-cue files as individual files, use --no-merge flag.
This is the default for vet. This flag only applies to data files
when used in combination with the --schema/-d flag.


Assigning values to a CUE path

The --path/-l flag can be used to specify a CUE path at which to
place a value. Each -l flag specifies either a CUE expression or
a CUE field (without the value following the colon), both of
which are evaluated within the value. Together, the -l flags
specify the path at increasingly deeper nesting. An expression
may refer to builtin packages as long as the name can be uniquely
identified.

The --with-context flag can be used to evaluate the label
expression within a struct of contextual data, instead of
within the value itself. This struct has the following fields:

{
	// data holds the original source data
	// (perhaps one of several records in a file).
	data: _
	// filename holds the full path to the file.
	filename: string
	// index holds the 0-based index element of the
	// record within the file. For files containing only
	// one record, this will be 0.
	index: uint & <recordCount
	// recordCount holds the total number of records
	// within the file.
	recordCount: int & >=1
}


Handling multiple documents or streams

To handle multi-document files, such as JSON Lines or YAML
files with document separators (---), the user must specify
a the --path, --list, or --files flag.
The --path flag merges each element into a single package as
if each element was defined in a separate file. The --list flag
concatenates each entry in a file into a list.
Using --list flag in combination with the --path flag
concatenates entries with the same path into a list, instead of
unifying them.
Finally, the --files option causes each entry to be written to
a different file. The -files flag may only be used in
combination with the import command.


Examples:

# Put a value at a path based on its "kind" and "name" fields.
$ cue eval -l 'strings.ToLower(kind)' -l name foo.yaml

# Include a schema under the "myschema" field using the path notation.
$ cue eval -l myschema: schema: foo.json

# Base the path values on its kind and file name.
$ cue eval --with-context -l 'path.Base(filename)' -l data.kind foo.yaml
`,
}

var filetypeHelp = &cobra.Command{
	Use:   "filetypes",
	Short: "supported file types and qualifiers",
	Long: `The cue tools supports the following file types:

    Tag         Extensions      Description
    cue         .cue            CUE source files.
    json        .json           JSON files.
    yaml        .yaml/.yml      YAML files.
    toml        .toml           TOML files
    jsonl       .jsonl/.ndjson  Line-separated JSON values.
    jsonschema                  JSON Schema.
    openapi                     OpenAPI schema.
	pb                          Use Protobuf mappings (e.g. json+pb)
    textproto    .textproto     Text-based protocol buffers.
    proto        .proto         Protocol Buffer definitions.
    go           .go            Go source files.
    text         .txt           Raw text file; the evaluated value
                                must be of type string.
    binary                      Raw binary file; the evaluated value
                                must be of type string or bytes.

OpenAPI, JSON Schema and Protocol Buffer definitions are
always interpreted as schema. YAML and JSON are always
interpreted as data. CUE and Go are interpreted as schema by
default, but may be selected to operate in data mode.

The cue tool will infer a file's type from its extension by
default. The user my override this behavior by using qualifiers.
A qualifier takes the form

    <tag>{'+'<tag>}':'

For instance,

	cue eval json: foo.data

specifies that 'foo.data' should be read as a JSON file. File
formats that do not have a default extension may be represented
in any data format using the same notation:

   cue def jsonschema: bar.cue foo.yaml openapi+yaml: baz.def

interprets the files bar.cue and foo.yaml as data in the
respective formats encoding an JSON Schema, while 'baz.def' is
defined to be a YAML file which contents encode OpenAPI
definitions.

A qualifier applies to all files following it on the command line
until the next qualifier. The cue tool does not allow a ':' in
filenames.

The following tags can be used in qualifiers to further
influence input or output. For input these act as
restrictions, validating the input. For output these act
as filters, showing only the requested data and picking
defaults as requested.

    Tag         Description
    data        Require concrete input and output that does
                not require any evaluation.
    graph       Like data, but allow references.
    schema      Export data and definitions.

The following tags are only valid in combination with other tags,
and influence the functioning of the codec. The tag they are
valid with is mentioned in parentheses at the end.

    strictFeatures	report errors for lossy mappings. Enabled by default (jsonschema)
    strictKeywords	report errors for unknown keywords (jsonschema)
    strict			report errors for either of the above (jsonschema)

The above flags also accept a boolean flag value (e.g. true, 1, false, 0)
to set them explicitly. For example, to ignore unimplemented JSON Schema
features rather than giving an error:

	jsonschema+strictFeatures=0

Many commands also support the --out and --outfile/-o flags.
The --out flag specifies the output type using a qualifier
(without the ':'). The -o flag specifies an output file
possibly prefixed with a qualifier.

Examples:

# Interpret bar.cue and foo.yaml as OpenAPI data.
$ cue def openapi: bar.cue foo.yaml

# Write a CUE package as OpenAPI encoded as YAML, using
# an alternate file extension.
$ cue def -o openapi+yaml:foo.openapi

# Print the data for the current package as YAML.
$ cue export --out=yaml

# Print the string value of the "name" field as a string.
$ cue export -e name --out=text

# Write the string value of the "name" field to a text file.
$ cue export -e name -o=foo.txt

# Write the string value of the "name" field to a file foo.
$ cue export -e name -o=text:foo
`,
}

var injectHelp = &cobra.Command{
	Use:   "injection",
	Short: "inject files or values into specific fields for a build",
	Long: `Many of the cue commands allow injecting values or
selecting files from the command line using the --inject/-t flag.


Injecting files

A "build" attribute defines a boolean expression that causes a file
to only be included in a build if its expression evaluates to true.
There may only be a single @if attribute per file and it must
appear before a package clause, or before any CUE declarations
if there is no package clause.

The expression is a subset of CUE consisting only of identifiers
and the operators &&, ||, !, where identifiers refer to tags
defined by the user on the command line.

For example, the following file will only be included in a build
if the user includes the flag "-t prod" on the command line.

   // File prod.cue
   @if(prod)

   package foo

Ignoring files

An "ignore" attribute causes a file to be unconditionally excluded
from a build. The @ignore attribute must appear before a package
clause or before any other CUE syntax if there is no package clause.

For example:

    @ignore()

    // This file will be excluded for all purposes.

    package foo

Injecting values

The injection mechanism allows values to be injected into fields
that are not defined within the scope of a comprehension, list, or
optional field and that are marked with a "tag" attribute. For any
field of the form

   field: x @tag(key)

an "--inject key=value" flag will modify the field to

   field: x & "value"

By default, the injected value is treated as a string.
Alternatively, the "type" option allows a value to be interpreted
as an int, number, or bool. For instance, for a field

   field: x @tag(key,type=int)

the flag "-t key=2" modifies the field to

   field: x & 2

Valid values for type are "int", "number", "bool", and "string".

A tag attribute can also define shorthand values, which can be
injected into the fields without having to specify the key. For
instance, for

   environment: string @tag(env,short=prod|staging)

"-t prod" sets the environment field to the value "prod". It is
still possible to specify "-t env=prod" in this case.

Use the usual CUE constraints to limit the possible values of a
field. For instance

   environment: "prod" | "staging" @tag(env,short=prod|staging)

ensures the user may only specify "prod" or "staging".


Tag variables

The injection mechanism allows for the injection of system variables:
when variable injection is enabled, tags of the form

    @tag(dir,var=cwd)

will inject the named variable (here cwd) into the tag. An explicitly
set value for a tag using --inject/-t takes precedence over an
available tag variable.

The following variables are supported:

   now        current time in RFC3339 format.
   os         OS identifier of the current system. Valid values:
                aix       android   darwin    dragonfly
                freebsd   illumos   ios       js (wasm)
                linux     netbsd    openbsd   plan9
                solaris   windows
   arch       architecture identifier of the current system. Valid values:
                386        amd64        amd64p32  arm
                armbe      arm64        arm64be   loong64
                mips       mipsle       mips64    mips64le
                mips64p32  mips64p32le  ppc       ppc64
                ppc64le    riscv        riscv64   s390
                s390x      sparc        sparc64   wasm
   cwd        working directory
   username   current username
   hostname   current hostname
   rand       a random 128-bit integer
`,
}

var commandsHelp = &cobra.Command{
	Use:   "commands",
	Short: "user-defined commands",
	Long: `Workflow commands define actions on instances. For example, they
may specify how to upload a configuration to Kubernetes. Workflow
commands are defined directly in tool files, which are regular
CUE files within the same package with a filename ending in
_tool.cue.  These are typically defined at the module root so
that they apply to all instances.

Each workflow command consists of one or more tasks. A task may,
for example, load or write a file, consult a user on the command
line, fetch a web page, and so on. Each task has inputs and
outputs. Outputs are typically filled out by the task
implementation as the task completes.

Inputs of tasks my refer to outputs of other tasks. The cue tool
does a static analysis of the configuration and only starts tasks
that are fully specified. Upon completion of each task, cue
rewrites the instance, filling in the completed task, and
reevaluates which other tasks can now start, and so on until all
tasks have completed.

Available tasks can be found in the package documentation at

	https://pkg.go.dev/cuelang.org/go/pkg/tool?tab=subdirectories

Examples:

In this simple example, we define a workflow command called
"hello", which declares a single task called "print" which uses
"tool/exec.Run" to execute a shell command that echos output to
the terminal:

	$ cat <<EOF > hello_tool.cue
	package foo

	import "tool/exec"

	city: "Amsterdam"
	who: *"World" | string @tag(who)

	// Say hello!
	command: hello: {
		print: exec.Run & {
			cmd: "echo Hello \(who)! Welcome to \(city)."
		}
	}
	EOF

We run the "hello" workflow command like this:

	$ cue cmd hello
	Hello World! Welcome to Amsterdam.

	$ cue cmd --inject who=Jan hello
	Hello Jan! Welcome to Amsterdam.


In this example we declare the "prompted" workflow command which
has four tasks. The first task prompts the user for a string
input. The second task depends on the first, and echos the
response back to the user with a friendly message. The third task
pipes the output from the second to a file. The fourth task pipes
the output from the second to standard output (i.e. it echos it
again).

	package foo

	import (
		"tool/cli"
		"tool/exec"
		"tool/file"
	)

	city: "Amsterdam"

	// Say hello!
	command: prompter: {
		// save transcript to this file
		var: file: *"out.txt" | string @tag(file)

		ask: cli.Ask & {
			prompt:   "What is your name?"
			response: string
		}

		// starts after ask
		echo: exec.Run & {
			cmd:    ["echo", "Hello", ask.response + "!"]
			stdout: string // capture stdout
		}

		// starts after echo
		append: file.Append & {
			filename: var.file
			contents: echo.stdout
		}

		// also starts after echo
		print: cli.Print & {
			text: echo.stdout
		}
	}

The types of the commands and tasks are defined in CUE itself at
cuelang.org/go/pkg/tool/tool.cue.

	command: [Name]: Command

	Command: {
		// Tasks specifies the things to run to complete a command. Tasks are
		// typically underspecified and completed by the particular internal
		// handler that is running them. Tasks can be a single task, or a full
		// hierarchy of tasks.
		//
		// Tasks that depend on the output of other tasks are run after such tasks.
		// Use $after if a task needs to run after another task but does not
		// otherwise depend on its output.
		Tasks

		// $usage summarizes how a command takes arguments.
		//
		// Example:
		//     mycmd [-n] names
		$usage?: string

		// $short is short description of what the command does.
		$short?: string

		// $long is a longer description that spans multiple lines and
		// likely contain examples of usage of the command.
		$long?: string
	}

	// Tasks defines a hierarchy of tasks. A command completes if all
	// tasks have run to completion.
	Tasks: Task | {
		[name=Name]: Tasks
	}

	// Name defines a valid task or command name.
	Name: =~#"^\PL([-](\PL|\PN))*$"#

	// A Task defines a step in the execution of a command.
	Task: {
		$type: "tool.Task" // legacy field 'kind' still supported for now.

		// $id indicates the operation to run. It must be of the form
		// packagePath.Operation.
		$id: =~#"\."#

		// $after can be used to specify a task is run after another one, when
		// it does not otherwise refer to an output of that task.
		$after?: Task | [...Task]
	}
`,
}

// indent returns s with all lines indented by one tab.
func indent(s string) string {
	var buf strings.Builder
	for scan := bufio.NewScanner(strings.NewReader(s)); scan.Scan(); {
		line := scan.Bytes()
		if len(line) == 0 {
			buf.WriteString("\n")
			continue
		}
		buf.WriteString("\t")
		buf.Write(line)
		buf.WriteString("\n")
	}
	return buf.String()
}

// TODO: tags
// - doc/nodoc
// - attr/noattr
// - id=<url>

// TODO: filetypes:
// - binpb
