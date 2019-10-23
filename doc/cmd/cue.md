# `cue` command reference

This documentation is a formatted from of the builtin documentation of the
cue help command.

## General usage

```
cue [command]
```

The commands are:

```
  cmd         run a user-defined shell command
  eval        evaluate a configuration file
  export      outputs an evaluated configuration in a standard format
  extract     
  fmt         formats Cue configuration files.
  help        Help about any command
  import      convert other data formats to CUE files
  list        list packages or modules
  serve       starts a builtin or user-defined service

Flags:
      --config string   config file (default is $HOME/.cue)
      --debug           give detailed error info
  -h, --help            help for cue
      --pkg string      CUE package to evaluate
      --root            load a Cue package from its root
```

## Start a bug report

TODO

## Evaluate a CUE file

```
Flags:
      --pkg string      Cue package to evaluate
```

## Formatting CUE files

`fmt` formats the given files or the files for the given packages in place.

Usage:
```
  cue fmt [-s] [packages] [flags]
```
Flags:
```
      --config string    config file (default is $HOME/.cue)
      --debug            give detailed error info
  -n, --dryrun           only run simulation
  -p, --package string   Cue package to evaluate
  -s, --simplify         simplify output
```

## Exporting results

`export` evaluates the configuration found in the current directory
and prints the emit value to stdout.

Examples:
Evaluated and emit

```
 # a single file
 cue export config.cue

 # multiple files: these are combined at the top-level.
 # Order doesn't matter.
 cue export file1.cue foo/file2.cue

 # all files within the "mypkg" package: this includes all files
 # in the current directory and its ancestor directories that are marked
 # with the same package.
 cue export -p cloud

 # the -p flag can be omitted if the directory only contains files for
 # the "mypkg" package.
 cue export
```

### Emit value

For CUE files, the generated configuration is derived from the top-level single expression, the emit value. For example, the file

```
 // config.cue
 arg1: 1
 arg2: "my string"

 {
  a: arg1
  b: arg2
 }
 ```
yields the following JSON:
```
 {
  "a": 1,
  "b": "my string"
 }
```
In absence of arguments, the current directory is loaded as a package instance.
A package instance for a directory contains all files in the directory and its
ancestor directories, up to the module root, belonging to the same package.

If the package is not explicitly defined by the '-p' flag, it must be uniquely
defined by the files in the current directory.

## Import files in another format

`import` converts other data formats, like JSON and YAML to CUE files.
The following file formats are currently supported:

```
  Format     Extensions
    JSON       .json .jsonl .ndjson
    YAML       .yaml .yml
```

Files can either be specified explicitly, or inferred from the specified
packages.
In either case, the file extension is replaced with `.cue`.
It will fail if the file already exists by default.
The -f flag overrides this.

Examples:

```
  # Convert individual files:
  $ cue import foo.json bar.json  # create foo.yaml and bar.yaml

  # Convert all json files in the indicated directories:
  $ cue import ./... -type=json
```


### The `--path` flag

By default the parsed files are included as emit values.
This default can be overridden by specifying a path, which has two forms:

```
 -p ident*
```

 and
```
 -p ident "->" expr*
```

The first form specifies a fixed path.
The empty path indicates including the value at the root.
The second form allows expressing the path in terms of the imported value.
An unbound identifier in the second form denotes a fixed name.
Packages may be included with the -imports flag.
Imports for top-level core packages are elided.


### Handling multiple documents or streams

To handle Multi-document files, such as concatenated JSON objects or YAML files
with document separators (---) the user must specify either the -path, -list, or
-files flag.
The -path flag assign each element to a path (identical paths are
treated as usual); -list concatenates the entries, and -files causes each entry
to be written to a different file.
The -files flag may only be used if files are explicitly imported.
The -list flag may be used in combination with the -path
flag, concatenating each entry to the mapped location.

Examples:

```
  $ cat <<EOF > foo.yaml
  kind: Service
  name: booster
  EOF

  # include the parsed file as an emit value:
  $ cue import foo.yaml
  $ cat foo.cue
  {
      kind: Service
      name: booster
  }

  # include the parsed file at the root of the Cue file:
  $ cue import -f -p "" foo.yaml
  $ cat foo.cue
  kind: Service
  name: booster

  # include the import config at the mystuff path
  $ cue import -f -p mystuff foo.yaml
  $ cat foo.cue
  myStuff: {
      kind: Service
      name: booster
  }

  # append another object to the input file
  $ cat <<EOF >> foo.yaml
  ---
  kind: Deployment
  name: booster
  replicas: 1

  # base the path values on th input
  $ cue import -f -p "x -> strings.ToLower(x.kind) x.name" foo.yaml
  $ cat foo.cue
  service booster: {
      kind: "Service"
      name: "booster"
  }

  deployment booster: {
      kind:     "Deployment"
      name:     "booster
      replicas: 1
  }

  # base the path values on th input
  $ cue import -f -list -foo.yaml
  $ cat foo.cue
  [{
      kind: "Service"
      name: "booster"
  }, {
      kind:     "Deployment"
      name:     "booster
      replicas: 1
  }]

  # base the path values on th input
  $ cue import -f -list -p "x->strings.ToLower(x.kind)" foo.yaml
  $ cat foo.cue
  service: [{
      kind: "Service"
      name: "booster"
  }
  deployment: [{
      kind:     "Deployment"
      name:     "booster
      replicas: 1
  }]
```

Usage:
```
  cue import [flags]
```

Flags:
```
      --dryrun        force overwriting existing files
      --files         force overwriting existing files
  -f, --force         force overwriting existing files
  -h, --help          help for import
      --list          concatenate multiple objects into a list
  -n, --name string   glob filter for file names
  -o, --out string    alternative output or - for stdout
  -p, --path string   path to include root
      --type string   only apply to files of this type
```

## Extracting CUE files from source code

TODO: doc

## Running scripts with CUE

`cmd` executes user-defined named commands for each of the listed instances.

Commands define actions on instances.
For instance, they may define how to upload a configuration to Kubernetes.
Commands are defined in cue files ending with `_tool.cue` while otherwise using
the same packaging rules: tool files must have a matching package clause and
the same rules as for normal files define which will be included in which
package.
Tool files have access to the package scope, but none of the fields defined
in a tool file influence the output of a package.
Tool files are typically defined at the module root so that they apply
to all instances.


### Tasks

Each command consists of one or more tasks.
A task may load or write a file, consult a user on the command line,
or fetch a web page, and so on.
Each task has inputs and outputs.
Outputs are typically are typically filled out by the task implementation as
the task completes.

Inputs of tasks my refer to outputs of other tasks.
The cue tool does a static analysis of the configuration and only starts tasks
that are fully specified.
Upon completion of each task, cue rewrites the instance,
filling in the completed task, and reevaluates which other tasks can now start,
and so on until all tasks have completed.


### Command definition

Commands are defined at the top-level of the configuration and all follow the
following pattern:

```
command <Name>: { // from "cue/tool".Command
  // usage gives a short usage pattern of the command.
  // Example:
  //    fmt [-n] [-x] [packages]
  usage: Name | string

  // short gives a brief on-line description of the command.
  // Example:
  //    reformat package sources
  short: "" | string

  // long gives a detailed description of the command, including
  // a description of flags usage and examples.
  long: "" | string

  // A task defines a single action to be run as part of this command.
  // Each task can have inputs and outputs, depending on the type
  // task. The outputs are initially unspecified, but are filled out
  // by the tooling
  //
  task <Name>: { // from "cue/tool".Task
   // supported fields depend on type
  }

  VarValue = string | bool | int | float | [...string|int|float]

  // var declares values that can be set by command line flags or
  // environment variables.
  //
  // Example:
  //   // environment to run in
  //   var env: "test" | "prod"
  // The tool would print documentation of this flag as:
  //   Flags:
  //      --env string    environment to run in: test(default) or prod
  var <Name>: VarValue

  // flag defines a command line flag.
  //
  // Example:
  //   var env: "test" | "prod"
  //
  //   // augment the flag information for var
  //   flag env: {
  //       shortFlag:   "e"
  //       description: "environment to run in"
  //   }
  //
  // The tool would print documentation of this flag as:
  //   Flags:
  //     -e, --env string    environment to run in: test(default), staging, or prod
  //
  flag <Name>: { // from "cue/tool".Flag
   // value defines the possible values for this flag.
   // The default is string. Users can define default values by
   // using disjunctions.
   value: env[Name].value | VarValue

   // name, if set, allows var to be set with the command-line flag
   // of the given name. null disables the command line flag.
   name: Name | null | string

   // short defines an abbreviated version of the flag.
   // Disabled by default.
   short: null | string
  }

  // populate flag with the default values for
  flag: { "\(k)": { value: v } | null for k, v in var }

  // env defines environment variables. It is populated with values
  // for var.
  //
  // To specify a var without an equivalent environment variable,
  // either specify it as a flag directly or disable the equally
  // named env entry explicitly:
  //
  //     var foo: string
  //     env foo: null  // don't use environment variables for foo
  //
  env <Name>: {
   // name defines the environment variable that sets this flag.
   name: "CUE_VAR_" + strings.Upper(Name) | string | null

   // The value retrieved from the environment variable or null
   // if not set.
   value: null | string | bytes
  }
  env: { "\(k)": { value: v } | null for k, v in var }
 }
```

Available tasks can be found in the package documentation at

```
 cmd/cue/tool/tasks.
 ```

More on tasks can be found in the tasks topic.

Examples
A simple file using command line execution:

hello.cue:
```
 package foo

 import "cue/tool/tasks/exec"

 city: "Amsterdam"
```

hello_tool.cue:
```
 package foo

 // Say hello!
 command hello: {
  // whom to say hello to
  var who: "World" | string

  task print: exec.Run({
   cmd: "echo Hello \(var.who)! Welcome to \(city)."
  })
 }
 ```

Invoking the script on the command line:

```
 $ cue cmd echo
 Hello World! Welcome to Amsterdam.

 $ cue cmd echo -who you
 Hello you! Welcome to Amsterdam.
 ```
An example with tasks depending on each other:

```
package foo

import "cue/tool/tasks/exec"

city: "Amsterdam"

// Say hello!
command hello: {
  var file: "out.txt" | string // save transcript to this file

  task ask: cli.Ask({
   prompt:   "What is your name?"
   response: string
  })

  // starts after ask
  task echo: exec.Run({
   cmd:    "echo Hello \(task.ask.response)!"
   stdout: string // capture stdout
  })

  // starts after echo
  task write: file.Append({
   filename: var.file
   contents: task.echo.stdout
  })

  // also starts after echo
  task print: cli.Print({
   contents: task.echo.stdout
  })
}
```
