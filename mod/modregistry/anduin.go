// Copyright (C) 2014-2024 Anduin Transactions Inc.
//
// Anduin maintained source code to patch OCI client behavior

package modregistry

import (
	"context"
	"log"
	"os"
	"strconv"
)

var logging, _ = strconv.ParseBool(os.Getenv("ANDUIN_CUE_DEBUG"))

type anduinPatch struct {
	originalClient *Client
}

// PutModuleWithMetadata
// Override default put module function to make restructure OCI layer. The goal is to make it compatible with both Oras and Cue cli
// The expected layers are as following:
//  1. ZIP file with only *.cue file included (compatible layer with cue)
//  2. Cue module file (compatible layer with cue)
//  3. All file with annotations of oras
//
// Note: *.cue will be duplicated because Oras will not pull cue layers
func (p *anduinPatch) putCheckedModule(ctx context.Context, m *checkedModule, meta *Metadata) error {
	logf("using patched `putCheckedModule`")
	return p.originalClient.putCheckedModule(ctx, m, meta)
}

func logf(f string, a ...any) {
	if logging {
		log.Printf("anduin: "+f, a...)
	}
}
