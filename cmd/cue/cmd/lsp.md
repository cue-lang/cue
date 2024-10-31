## `cue lsp`

Some WIP notes on running or contributing to `cue lsp`.

### Requirements/limitations

* A WorkspaceFolder must be rooted in a directory that exists in the directory
  hierarchy of a valid CUE module. A valid CUE module is rooted at a directory
  that contains a directory named `cue.mod`.

### Running `cue lsp` - VSCode

* Use the `Output` window and select the specific `cue lsp` log for log
  messages. If you need trace-level logging of the LSP, then modify the settings
  of `vscode-cue` (or the folder or workspace settings) to add flags to specify
  a logfile location and the level of tracing:

```
{
   "cue.languageServerFlags" : [
      "-logfile=/tmp/cue_lsp.vscode",
      "-rpc.trace"
   ]
}
```

### Running `cue lsp` - Neovim

* Use `:LspLog` for log messages.
* Use `jjo/vim-cue` for syntax highlighting. Look to establish a CUE-maintained
  Vim plugin for this purpose.

### Contributing

* Running integration tests: use `-print_logs` for full trace-level logging of
  the LSP interactions. Combine that with `-run` in order to debug the
  interactions of a single test, e.g.

```
# cmd/cue/cmd/integration/base
go test -print_logs -run TestFormatFile/default
```

* Integration tests set `verboseWorkDoneProgress: true` in order to be able to
  await/assert based on async events. For example, the initial workspace load.
  Therefore, take care when making changes to code guarded by
  `s.Options().VerboseWorkDoneProgress` or similar, because it likely has code
  in the integration tests that depends on it indirectly via awaiters or similar
  that are spying on output in the RPC trace to detect certain events having
  happened.


### VSCode vs LSP vs `cue lsp` nomenclature

This section is a WIP series of notes that should help new/existing contributors
to refresh on key concepts that are somewhat easily forgotten. Notes written and
supplemented by ChatGPT, so not all errors are ours.

- VSCode introduces the concept of a **workspace**, which can be saved as a
  `.code-workspace` file. This file contains settings and configuration for the
  workspace, such as project-specific settings, extensions, and workspace
  folders.
- If you open a folder directly in VSCode without using a `.code-workspace`
  file, that folder becomes the "default workspace." You can consider this as a
  single-folder workspace.
- A **VSCode workspace** can consist of one or more open folders. In a
  **single-folder workspace**, only one folder is open. In a **multi-root
  workspace**, multiple folders are open, and all of them are treated as part of
  the same workspace context.
- A **folder in VSCode** corresponds to a **WorkspaceFolder** in the LSP
  specification. This is the concept that LSP uses to describe directories
  (i.e., root paths) that the language server should monitor or process.
- The LSP specification does not have a direct concept that maps exactly to a
  **VSCode workspace**. Instead, the LSP treats the folders within a workspace
  as **workspace folders**.
- When a language server is initialized, the server receives information about
  the workspace folders in the **`workspaceFolders`** parameter during the
  initialization process, corresponding to the folders open in VSCode.
- Both Neovim and VSCode start an LSP instance in response to certain activation
  events. For now, both activate when a `.cue` file is opened.
- VSCode's concept of a **folder** corresponds directly to an LSP
  **WorkspaceFolder**. Hence if you were to open a folder in VSCode root at a
  subdirectory of a CUE module, then the root of the WorkspaceFolder would be
  that subdirectory.
- Neovim behaves slightly differently, discovering a root directory from the CWD
  according to matching conditions. More understanding and nuance needed here
  most likely.
- Whilst the LSP spec does not formally define the concept of a **workspace**,
  it does define a number of methods/notifications in the `workspace/`
  namespace. This grouping is in effect the definition of a workspace. For
  example, the client notifying the server via
  `workspace/didChangeWorkspaceFolders` is necessarily a higher level concept
  than a WorkspaceFolder, because it is changing the set of WorkspaceFolders.
  And this is, by construction, the concept of a workspace in LSP: the space in
  which things that span/define WorkspaceFolders are done.
- In `cue lsp`, the `server` has a single `Session`. The `Session` holds `Views`,
  which are the equivalent of `WorkspaceFolder`'s. A `Snapshot` represents the
  current state for a given `View`.

### LSP Client Requirements/Restrictions

* LSP clients that speak to `cue lsp` must support the concept of
  WorkspaceFolder. For now, we only support a single WorkspaceFolder.
