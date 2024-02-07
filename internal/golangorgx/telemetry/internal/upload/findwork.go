// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"os"
	"path/filepath"
	"strings"

	it "cuelang.org/go/internal/golangorgx/telemetry/internal/telemetry"
)

// files to handle
type work struct {
	// absolute file names
	countfiles []string // count files to process
	readyfiles []string // old reports to upload
	// relative names
	uploaded map[string]bool // reports that have been uploaded
}

// find all the files that look like counter files or reports
// that need to be uploaded. (There may be unexpected leftover files
// and uploading is supposed to be idempotent.)
func findWork(localdir, uploaddir string) work {
	var ans work
	fis, err := os.ReadDir(localdir)
	if err != nil {
		logger.Printf("could not read %s, progress impossible (%v)", localdir, err)
		return ans
	}

	mode, asof := it.Mode()

	// count files end in .v1.count
	// reports end in .json. If they are not to be uploaded they
	// start with local.
	for _, fi := range fis {
		if strings.HasSuffix(fi.Name(), ".v1.count") {
			fname := filepath.Join(localdir, fi.Name())
			if stillOpen(fname) {
				continue
			}
			ans.countfiles = append(ans.countfiles, fname)
		} else if strings.HasPrefix(fi.Name(), "local.") {
			// skip
		} else if strings.HasSuffix(fi.Name(), ".json") && mode == "on" {
			// Collect reports that are ready for upload.
			reportDate := uploadReportDate(fi.Name())
			if !asof.IsZero() && !reportDate.IsZero() {
				// If both the mode asof date and the report date are present, do the
				// right thing...
				//
				// (see https://github.com/golang/go/issues/63142#issuecomment-1734025130)
				if asof.Before(reportDate) {
					// Note: since this report was created after telemetry was enabled,
					// we can only assume that the process that created it checked that
					// the counter data contained therein was all from after the asof
					// date.
					//
					// TODO(rfindley): store the begin date in reports, so that we can
					// verify this assumption.
					ans.readyfiles = append(ans.readyfiles, filepath.Join(localdir, fi.Name()))
				}
			} else {
				// ...otherwise fall back on the old behavior of uploading all
				// unuploaded files.
				//
				// TODO(rfindley): invert this logic following more testing. We
				// should only upload if we know both the asof date and the report
				// date, and they are acceptable.
				ans.readyfiles = append(ans.readyfiles, filepath.Join(localdir, fi.Name()))
			}
		}
	}

	fis, err = os.ReadDir(uploaddir)
	if err != nil {
		os.MkdirAll(uploaddir, 0777)
		return ans
	}
	// There should be only one of these per day; maybe sometime
	// we'll want to clean the directory.
	ans.uploaded = make(map[string]bool)
	for _, fi := range fis {
		if strings.HasSuffix(fi.Name(), ".json") {
			ans.uploaded[fi.Name()] = true
		}
	}
	return ans
}
