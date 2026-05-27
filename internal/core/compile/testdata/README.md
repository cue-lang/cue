# Compile Testdata

The `sync` directory is generated from `cue/testdata` when running:

```sh
CUE_UPDATE=1 go test ./internal/core/compile
```

Do not edit files under `sync` directly. The update run deletes and recreates
that directory, then regenerates the compile output sections.

Compile-specific test archives that should not be synced from `cue/testdata`
belong outside `sync`, for example under `local/`.
