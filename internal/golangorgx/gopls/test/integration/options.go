// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"maps"

	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
	"cuelang.org/go/internal/lsp/cache"
)

type runConfig struct {
	editor  fake.EditorConfig
	sandbox fake.SandboxConfig
	modes   Mode

	// initializeErrorMatches is set to a non-empty string if it is expected that
	// creating a new fake editor will fail with an error during the Initialize
	// sequence of the LSP protocol.
	//
	// TODO(myitcv): is there a better place for this?
	initializeErrorMatches string
	reg                    cache.Registry
}

func defaultConfig() runConfig {
	return runConfig{
		editor: fake.EditorConfig{
			Settings: map[string]interface{}{
				// Shorten the diagnostic delay to speed up test execution (else we'd add
				// the default delay to each assertion about diagnostics)
				"diagnosticsDelay": "10ms",
			},
		},
	}
}

// A RunOption augments the behavior of the test runner.
type RunOption interface {
	set(*runConfig)
}

type optionSetter func(*runConfig)

func (f optionSetter) set(opts *runConfig) {
	f(opts)
}

// Modes configures the execution modes that the test should run in.
//
// By default, modes are configured by the test runner. If this option is set,
// it overrides the set of default modes and the test runs in exactly these
// modes.
func Modes(modes Mode) RunOption {
	return optionSetter(func(opts *runConfig) {
		if opts.modes != 0 {
			panic("modes set more than once")
		}
		opts.modes = modes
	})
}

// CapabilitiesJSON sets the capabalities json.
func CapabilitiesJSON(capabilities []byte) RunOption {
	return optionSetter(func(opts *runConfig) {
		opts.editor.CapabilitiesJSON = capabilities
	})
}

// Settings sets user-provided configuration for the LSP server.
type Settings map[string]interface{}

func (s Settings) set(opts *runConfig) {
	if opts.editor.Settings == nil {
		opts.editor.Settings = make(map[string]interface{})
	}
	maps.Copy(opts.editor.Settings, s)
}

// WorkspaceFolders configures the workspace folders to send to the LSP
// server, as workdir-relative paths or as URIs such as virtual:///path.
// By default the editor sends a single workspace folder corresponding to
// the workdir root. To explicitly configure no workspace folders, use
// WorkspaceFolders with no arguments.
func WorkspaceFolders(relFolders ...string) RunOption {
	if len(relFolders) == 0 {
		// Use an empty non-nil slice to signal explicitly no folders.
		relFolders = []string{}
	}

	return optionSetter(func(opts *runConfig) {
		opts.editor.WorkspaceFolders = relFolders
	})
}

// RootURIAsDefaultFolder configures the RootURI initialization
// parameter to be set to the first workspace folder, as VS Code does,
// or to the default workdir root if there are no workspace folders.
// This is sent to the LSP server as part of initialization.
func RootURIAsDefaultFolder() RunOption {
	return optionSetter(func(opts *runConfig) {
		opts.editor.RootURIAsDefaultFolder = true
	})
}

// The InitializeError RunOption establishes the expectation that creating a
// new fake editor (happens as part of Run) will fail with the non-empty error
// string err.
func InitializeError(err string) RunOption {
	return optionSetter(func(opts *runConfig) {
		opts.initializeErrorMatches = err
	})
}

// FolderSettings defines per-folder workspace settings, keyed by relative path
// to the folder.
//
// Use in conjunction with WorkspaceFolders to have different settings for
// different folders.
type FolderSettings map[string]Settings

func (fs FolderSettings) set(opts *runConfig) {
	// Re-use the Settings type, for symmetry, but translate back into maps for
	// the editor config.
	folders := make(map[string]map[string]any)
	for k, v := range fs {
		folders[k] = v
	}
	opts.editor.FolderSettings = folders
}

// Registry configures the server to use the provided [cache.Registry]
// instead of the default registry.
func Registry(reg cache.Registry) RunOption {
	return optionSetter(func(opts *runConfig) {
		opts.reg = reg
	})
}
