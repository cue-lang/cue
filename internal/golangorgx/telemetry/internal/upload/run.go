// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"io"
	"log"
	"time"

	"cuelang.org/go/internal/golangorgx/telemetry"
	it "cuelang.org/go/internal/golangorgx/telemetry/internal/telemetry"
)

var logger *log.Logger

func init() {
	logger = log.New(io.Discard, "", 0)
}

// SetLogOutput sets the default logger's output destination.
func SetLogOutput(logging io.Writer) {
	if logging != nil {
		logger.SetOutput(logging)
	}
}

// Uploader carries parameters needed for upload.
type Uploader struct {
	// Config is used to select counters to upload.
	Config *telemetry.UploadConfig
	// ConfigVersion is the version of the config.
	ConfigVersion string

	// LocalDir is where the local counter files are.
	LocalDir string
	// UploadDir is where uploader leaves the copy of uploaded data.
	UploadDir string
	// ModeFilePath is the file.
	ModeFilePath it.ModeFilePath

	UploadServerURL string
	StartTime       time.Time

	cache parsedCache
}

// NewUploader creates a default uploader.
func NewUploader(config *telemetry.UploadConfig) *Uploader {
	return &Uploader{
		Config:          config,
		ConfigVersion:   "custom",
		LocalDir:        it.LocalDir,
		UploadDir:       it.UploadDir,
		ModeFilePath:    it.ModeFile,
		UploadServerURL: "https://telemetry.go.dev/upload",
		StartTime:       time.Now().UTC(),
	}
}

// Run generates and uploads reports
func (u *Uploader) Run() {
	todo := u.findWork()
	ready, err := u.reports(&todo)
	if err != nil {
		logger.Printf("reports: %v", err)
	}
	for _, f := range ready {
		u.uploadReport(f)
	}
}
