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

package adt

import (
	"regexp"
	"sync"
	"testing"
)

// BenchmarkRegexpWeakMapCache benchmarks the new WeakMap-based caching approach.
// This caches compiled regexps in a thread-safe WeakMap keyed by pattern string.
func BenchmarkRegexpWeakMapCache(b *testing.B) {
	pattern := `^[a-zA-Z][a-zA-Z0-9_]*$`
	// Warm up - ensure pattern is cached
	_, _ = cachedRegexp(pattern)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cachedRegexp(pattern)
	}
}

// BenchmarkRegexpWeakMapCacheConcurrent benchmarks concurrent access to the cache.
func BenchmarkRegexpWeakMapCacheConcurrent(b *testing.B) {
	pattern := `^[a-zA-Z][a-zA-Z0-9_]*$`
	_, _ = cachedRegexp(pattern)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = cachedRegexp(pattern)
		}
	})
}

// BenchmarkRegexpCompile benchmarks direct regexp compilation (no caching).
func BenchmarkRegexpCompile(b *testing.B) {
	pattern := `^[a-zA-Z][a-zA-Z0-9_]*$`
	for i := 0; i < b.N; i++ {
		_, _ = regexp.Compile(pattern)
	}
}

func TestCachedRegexpConcurrent(t *testing.T) {
	// Test that concurrent access doesn't cause races
	pattern := `^test\d+$`
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				re, err := cachedRegexp(pattern)
				if err != nil {
					t.Error(err)
					return
				}
				if !re.MatchString("test123") {
					t.Error("regexp should match")
					return
				}
			}
		}()
	}
	wg.Wait()
}
