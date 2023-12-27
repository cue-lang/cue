// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"io"
	"log"

	"cuelang.org/go/internal/golangorgx/telemetry/internal/upload"
)

// Run generates and uploads reports, as allowed by the mode file.
// A nil Control is legal.
func Run(c *Control) {
	if c != nil && c.Logging != nil {
		upload.SetLogOutput(c.Logging)
	}
	// ignore error: failed logging should not block uploads
	upload.LogIfDebug("")

	defer func() {
		if err := recover(); err != nil {
			log.Printf("upload recover: %v", err)
		}
	}()
	upload.NewUploader(nil).Run()
}

// A Control allows the user to override various default
// reporting and uploading choices.
// Future versions may also allow the user to set the upload URL.
type Control struct {
	// Logging provides a io.Writer for error messages during uploading
	// nil is legal and no log messages get generated
	Logging io.Writer
}
