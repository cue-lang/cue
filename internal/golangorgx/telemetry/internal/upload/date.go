// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"fmt"
	"os"
	"sync"
	"time"

	"cuelang.org/go/internal/golangorgx/telemetry/internal/counter"
)

// time and date handling

// all the upload processing takes place (conceptually) at
// a single instant. Most of the time this wouldn't matter
// but it protects against time skew if time.Now
// increases the day between calls, as might happen (rarely) by chance
// or if there are long scheduling delays between calls.
var thisInstant = time.Now().UTC()

var distantPast = 21 * 24 * time.Hour

// reports that are too old (21 days) are not uploaded
func tooOld(date string) bool {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		logger.Printf("tooOld: %v", err)
		return false
	}
	age := thisInstant.Sub(t)
	return age > distantPast
}

// a time in the far future for the expiry time with errors
var farFuture = time.UnixMilli(1 << 62)

// counterDateSpan parses the counter file named fname and returns the (begin, end) span
// recorded in its metadata.
// On any error, it returns (0, farFuture), so that invalid files don't look
// like they can be used.
//
// TODO(rfindley): just return an error to make this explicit.
func counterDateSpan(fname string) (begin, end time.Time) {
	parsed, err := parse(fname)
	if err != nil {
		logger.Printf("expiry Parse: %v for %s", err, fname)
		return time.Time{}, farFuture
	}
	begin, err = time.Parse(time.RFC3339, parsed.Meta["TimeBegin"])
	if err != nil {
		logger.Printf("time.Parse(%s[TimeBegin]) failed: %v", fname, err)
		return time.Time{}, farFuture
	}
	end, err = time.Parse(time.RFC3339, parsed.Meta["TimeEnd"])
	if err != nil {
		logger.Printf("time.Parse(%s[TimeEnd]) failed: %v", fname, err)
		return time.Time{}, farFuture
	}
	return begin, end
}

// stillOpen returns true if the counter file might still be active
func stillOpen(fname string) bool {
	_, expiry := counterDateSpan(fname)
	return expiry.After(thisInstant)
}

// avoid parsing count files multiple times
type parsedCache struct {
	mu sync.Mutex
	m  map[string]*counter.File
}

var cache parsedCache

func parse(fname string) (*counter.File, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.m == nil {
		cache.m = make(map[string]*counter.File)
	}
	if f, ok := cache.m[fname]; ok {
		return f, nil
	}
	buf, err := os.ReadFile(fname)
	if err != nil {
		return nil, fmt.Errorf("parse ReadFile: %v for %s", err, fname)
	}
	f, err := counter.Parse(fname, buf)
	if err != nil {

		return nil, fmt.Errorf("parse Parse: %v for %s", err, fname)
	}
	cache.m[fname] = f
	return f, nil
}
