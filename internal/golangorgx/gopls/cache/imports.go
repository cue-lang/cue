// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/keys"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
	"cuelang.org/go/internal/golangorgx/tools/imports"
)

// refreshTimer implements delayed asynchronous refreshing of state.
//
// See the [refreshTimer.schedule] documentation for more details.
type refreshTimer struct {
	mu        sync.Mutex
	duration  time.Duration
	timer     *time.Timer
	refreshFn func()
}

// newRefreshTimer constructs a new refresh timer which schedules refreshes
// using the given function.
func newRefreshTimer(refresh func()) *refreshTimer {
	return &refreshTimer{
		refreshFn: refresh,
	}
}

// schedule schedules the refresh function to run at some point in the future,
// if no existing refresh is already scheduled.
//
// At a minimum, scheduled refreshes are delayed by 30s, but they may be
// delayed longer to keep their expected execution time under 2% of wall clock
// time.
func (t *refreshTimer) schedule() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.timer == nil {
		// Don't refresh more than twice per minute.
		delay := 30 * time.Second
		// Don't spend more than ~2% of the time refreshing.
		if adaptive := 50 * t.duration; adaptive > delay {
			delay = adaptive
		}
		t.timer = time.AfterFunc(delay, func() {
			start := time.Now()
			t.refreshFn()
			t.mu.Lock()
			t.duration = time.Since(start)
			t.timer = nil
			t.mu.Unlock()
		})
	}
}

// A sharedModCache tracks goimports state for GOMODCACHE directories
// (each session may have its own GOMODCACHE).
//
// This state is refreshed independently of view-specific imports state.
type sharedModCache struct {
	mu     sync.Mutex
	caches map[string]*imports.DirInfoCache // GOMODCACHE -> cache content; never invalidated
	timers map[string]*refreshTimer         // GOMODCACHE -> timer
}

func (c *sharedModCache) dirCache(dir string) *imports.DirInfoCache {
	c.mu.Lock()
	defer c.mu.Unlock()

	cache, ok := c.caches[dir]
	if !ok {
		cache = imports.NewDirInfoCache()
		c.caches[dir] = cache
	}
	return cache
}

// refreshDir schedules a refresh of the given directory, which must be a
// module cache.
func (c *sharedModCache) refreshDir(ctx context.Context, dir string, logf func(string, ...any)) {
	cache := c.dirCache(dir)

	c.mu.Lock()
	defer c.mu.Unlock()
	timer, ok := c.timers[dir]
	if !ok {
		timer = newRefreshTimer(func() {
			_, done := event.Start(ctx, "cache.sharedModCache.refreshDir", tag.Directory.Of(dir))
			defer done()
			imports.ScanModuleCache(dir, cache, logf)
		})
		c.timers[dir] = timer
	}

	timer.schedule()
}

// importsState tracks view-specific imports state.
type importsState struct {
	ctx          context.Context
	modCache     *sharedModCache
	refreshTimer *refreshTimer

	mu                sync.Mutex
	processEnv        *imports.ProcessEnv
	cachedModFileHash file.Hash
}

// newImportsState constructs a new imports state for running goimports
// functions via [runProcessEnvFunc].
//
// The returned state will automatically refresh itself following a call to
// runProcessEnvFunc.
func newImportsState(backgroundCtx context.Context, modCache *sharedModCache, env *imports.ProcessEnv) *importsState {
	s := &importsState{
		ctx:        backgroundCtx,
		modCache:   modCache,
		processEnv: env,
	}
	s.refreshTimer = newRefreshTimer(s.refreshProcessEnv)
	return s
}

func (s *importsState) refreshProcessEnv() {
	ctx, done := event.Start(s.ctx, "cache.importsState.refreshProcessEnv")
	defer done()

	start := time.Now()

	s.mu.Lock()
	resolver, err := s.processEnv.GetResolver()
	s.mu.Unlock()
	if err != nil {
		return
	}

	event.Log(s.ctx, "background imports cache refresh starting")

	// Prime the new resolver before updating the processEnv, so that gopls
	// doesn't wait on an unprimed cache.
	if err := imports.PrimeCache(context.Background(), resolver); err == nil {
		event.Log(ctx, fmt.Sprintf("background refresh finished after %v", time.Since(start)))
	} else {
		event.Log(ctx, fmt.Sprintf("background refresh finished after %v", time.Since(start)), keys.Err.Of(err))
	}

	s.mu.Lock()
	s.processEnv.UpdateResolver(resolver)
	s.mu.Unlock()
}
