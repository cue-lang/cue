// Copyright 2025 CUE Authors
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

package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
)

// Options returns the current server options.
//
// The caller must not modify the result.
func (s *server) Options() *settings.Options {
	return s.options
}

// SetOptions sets the current server options.
//
// The caller must not subsequently modify the contents of opts.
func (s *server) SetOptions(opts *settings.Options) {
	s.options = opts
}

// handleOptionResults processes the results of combining existing
// options with options newly received from the editor/client. It is
// validation of options.
func (s *server) handleOptionResults(ctx context.Context, results settings.OptionResults) error {
	var warnings, errors []string
	for _, result := range results {
		switch result.Error.(type) {
		case nil:
			// nothing to do
		case *settings.SoftError:
			warnings = append(warnings, result.Error.Error())
		default:
			errors = append(errors, result.Error.Error())
		}
	}

	// Sort messages, but put errors first.
	//
	// Having stable content for the message allows clients to de-duplicate. This
	// matters because we may send duplicate warnings for clients that support
	// dynamic configuration: one for the initial settings, and then more for the
	// individual viewsettings.
	var msgs []string
	msgType := protocol.Warning
	if len(errors) > 0 {
		msgType = protocol.Error
		sort.Strings(errors)
		msgs = append(msgs, errors...)
	}
	if len(warnings) > 0 {
		sort.Strings(warnings)
		msgs = append(msgs, warnings...)
	}

	if len(msgs) > 0 {
		combined := "Invalid settings: " + strings.Join(msgs, "; ")
		params := &protocol.ShowMessageParams{
			Type:    msgType,
			Message: combined,
		}
		return s.eventuallyShowMessage(ctx, params)
	}

	return nil
}

// fetchFolderOptions makes a workspace/configuration request for the given
// folder, and populates options with the result.
//
// If dir is "", fetchFolderOptions makes an unscoped request.
func (s *server) fetchFolderOptions(ctx context.Context, dir protocol.DocumentURI) (*settings.Options, error) {
	options := s.Options()
	if !options.ConfigurationSupported {
		return options, nil
	}

	var scopeURI *string
	if dir != "" {
		scope := string(dir)
		scopeURI = &scope
	}
	configs, err := s.client.Configuration(ctx, &protocol.ParamConfiguration{
		Items: []protocol.ConfigurationItem{{
			ScopeURI: scopeURI,
			Section:  "cuelsp",
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace configuration from client (%s): %v", dir, err)
	}

	options = options.Clone()
	for _, config := range configs {
		if err := s.handleOptionResults(ctx, settings.SetOptions(options, config)); err != nil {
			return nil, err
		}
	}
	return options, nil
}
