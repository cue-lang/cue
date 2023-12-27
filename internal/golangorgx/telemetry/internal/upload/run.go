// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"io"
	"log"

	"cuelang.org/go/internal/golangorgx/telemetry"
	it "cuelang.org/go/internal/golangorgx/telemetry/internal/telemetry"
)

var logger *log.Logger

func init() {
	logger = log.New(io.Discard, "", 0)
}

// Run generates and uploads reports
func Run(c *telemetry.Control) {
	if c != nil {
		if c.UploadConfig != nil {
			uploadConfig = c.UploadConfig()
		}
		if c.Logging != nil {
			logger.SetOutput(c.Logging)
		}
	}
	todo := findWork(it.LocalDir, it.UploadDir)
	ready, err := reports(&todo)
	if err != nil {
		logger.Printf("reports: %v", err)
	}
	for _, f := range ready {
		uploadReport(f)
	}
}
