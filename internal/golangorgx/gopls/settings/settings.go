// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package settings

import (
	"fmt"
	"strings"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// Options holds various configuration that affects the LSP server.
type Options struct {
	// LSP-specific configuration provided by the client.
	ClientInfo                    *protocol.ClientInfo
	ConfigurationSupported        bool
	DynamicConfigurationSupported bool
	DynamicWatchedFilesSupported  bool
	CodeActionResolveOptions      []string

	// DiagnosticsDelay controls the amount of time that the server waits
	// after the most recent file modification before computing diagnostics.
	//
	// This option must be set to a valid duration string, for example `"250ms"`.
	DiagnosticsDelay time.Duration

	// VerboseWorkDoneProgress controls whether the LSP server should send
	// progress reports for all work done outside the scope of an RPC.
	// Used by the regression tests.
	VerboseWorkDoneProgress bool

	// SubdirWatchPatterns configures the file watching glob patterns registered
	// by the server.
	//
	// Some clients (namely VS Code) do not send workspace/didChangeWatchedFile
	// notifications for files contained in a directory when that directory is
	// deleted:
	// https://github.com/microsoft/vscode/issues/109754
	//
	// In this case, the server would miss important notifications about
	// deleted packages. To work around this, the server registers a watch
	// pattern for each directory.
	//
	// Unfortunately, other clients experience performance problems with this
	// many watch patterns, so there is no single behavior that works well for
	// all clients.
	//
	// The "subdirWatchPatterns" setting allows configuring this behavior. Its
	// default value of "auto" attempts to guess the correct behavior based on
	// the client name. We'd love to avoid this specialization, but as described
	// above there is no single value that works for all clients.
	SubdirWatchPatterns SubdirWatchPatterns
}

type SubdirWatchPatterns string

const (
	SubdirWatchPatternsOn   SubdirWatchPatterns = "on"
	SubdirWatchPatternsOff  SubdirWatchPatterns = "off"
	SubdirWatchPatternsAuto SubdirWatchPatterns = "auto"
)

type OptionResults []OptionResult

type OptionResult struct {
	Name  string
	Value any
	Error error
}

func SetOptions(options *Options, opts any) OptionResults {
	var results OptionResults
	switch opts := opts.(type) {
	case nil:
	case map[string]any:
		seen := map[string]struct{}{}
		for name, value := range opts {
			results = append(results, options.set(name, value, seen))
		}
	default:
		results = append(results, OptionResult{
			Value: opts,
			Error: fmt.Errorf("Invalid options type %T", opts),
		})
	}
	return results
}

func (o *Options) ForClientCapabilities(clientName *protocol.ClientInfo, caps protocol.ClientCapabilities) {
	o.ClientInfo = clientName
	// Check if the client supports configuration messages.
	o.ConfigurationSupported = caps.Workspace.Configuration
	o.DynamicConfigurationSupported = caps.Workspace.DidChangeConfiguration.DynamicRegistration
	o.DynamicWatchedFilesSupported = caps.Workspace.DidChangeWatchedFiles.DynamicRegistration

	// Check if the client supports code actions resolving.
	if caps.TextDocument.CodeAction.DataSupport && caps.TextDocument.CodeAction.ResolveSupport != nil {
		o.CodeActionResolveOptions = caps.TextDocument.CodeAction.ResolveSupport.Properties
	}
}

// Clone returns a copy of o. Its slices and pointers are shared with o,
// as they are never modified once set.
func (o *Options) Clone() *Options {
	result := *o
	return &result
}

func (o *Options) set(name string, value any, seen map[string]struct{}) OptionResult {
	// Flatten the name in case we get options with a hierarchy.
	split := strings.Split(name, ".")
	name = split[len(split)-1]

	result := OptionResult{Name: name, Value: value}
	if _, ok := seen[name]; ok {
		result.parseErrorf("duplicate configuration for %s", name)
	}
	seen[name] = struct{}{}

	switch name {
	case "verboseWorkDoneProgress":
		result.setBool(&o.VerboseWorkDoneProgress)

	case "diagnosticsDelay":
		result.setDuration(&o.DiagnosticsDelay)

	case "subdirWatchPatterns":
		if s, ok := result.asOneOf(
			string(SubdirWatchPatternsOn),
			string(SubdirWatchPatternsOff),
			string(SubdirWatchPatternsAuto),
		); ok {
			o.SubdirWatchPatterns = SubdirWatchPatterns(s)
		}

	default:
		result.unexpected()
	}
	return result
}

// parseErrorf reports an error parsing the current configuration value.
func (r *OptionResult) parseErrorf(msg string, values ...any) {
	r.Error = fmt.Errorf("parsing setting %q: %s", r.Name, fmt.Sprintf(msg, values...))
}

// unexpected reports that the current setting is not known to the server.
func (r *OptionResult) unexpected() {
	r.Error = fmt.Errorf("unexpected setting %q", r.Name)
}

func (r *OptionResult) setBool(b *bool) {
	v, ok := r.Value.(bool)
	if !ok {
		r.parseErrorf("invalid type %T, expect bool", r.Value)
		return
	}
	*b = v
}

func (r *OptionResult) setDuration(d *time.Duration) {
	if v, ok := r.asString(); ok {
		parsed, err := time.ParseDuration(v)
		if err != nil {
			r.parseErrorf("failed to parse duration %q: %v", v, err)
			return
		}
		*d = parsed
	}
}

func (r *OptionResult) asString() (string, bool) {
	b, ok := r.Value.(string)
	if !ok {
		r.parseErrorf("invalid type %T, expect string", r.Value)
		return "", false
	}
	return b, true
}

func (r *OptionResult) asOneOf(options ...string) (string, bool) {
	s, ok := r.asString()
	if !ok {
		return "", false
	}
	for _, opt := range options {
		if strings.EqualFold(opt, s) {
			return opt, true
		}
	}
	r.parseErrorf("invalid option %q for enum", s)
	return "", false
}
