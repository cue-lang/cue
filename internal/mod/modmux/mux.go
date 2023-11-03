// Copyright 2023 CUE Labs AG
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

package modmux

import (
	"context"
	"fmt"
	"io"
	"sync"

	"cuelabs.dev/go/oci/ociregistry"

	"cuelang.org/go/internal/mod/modresolve"
)

// New returns a registry implementation that uses the given
// resolver to multiplex between different registries.
//
// The newRegistry function will be used to create the
// registries for the hosts in the [modresolver.Location] values
// returned by the resolver.
//
// The returned registry always returns an error for Repositories and MountBlob
// (neither of these capabilities are required or used by the module fetching/pushing
// logic).
func New(resolver modresolve.Resolver, newRegistry func(host string, insecure bool) (ociregistry.Interface, error)) ociregistry.Interface {
	return &registry{
		resolver:    resolver,
		newRegistry: newRegistry,
		repos:       make(map[string]ociregistry.Interface),
	}
}

type registry struct {
	*ociregistry.Funcs
	resolver    modresolve.Resolver
	newRegistry func(host string, insecure bool) (ociregistry.Interface, error)

	mu    sync.Mutex
	repos map[string]ociregistry.Interface
}

func (r *registry) GetBlob(ctx context.Context, repo string, digest ociregistry.Digest) (ociregistry.BlobReader, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return nil, err
	}
	return cr.GetBlob(ctx, repo, digest)
}

func (r *registry) GetBlobRange(ctx context.Context, repo string, digest ociregistry.Digest, offset0, offset1 int64) (ociregistry.BlobReader, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return nil, err
	}
	return cr.GetBlobRange(ctx, repo, digest, offset0, offset1)
}

func (r *registry) GetManifest(ctx context.Context, repo string, digest ociregistry.Digest) (ociregistry.BlobReader, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return nil, err
	}
	return cr.GetManifest(ctx, repo, digest)
}

func (r *registry) GetTag(ctx context.Context, repo string, tagName string) (ociregistry.BlobReader, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return nil, err
	}
	return cr.GetTag(ctx, repo, tagName)
}

func (r *registry) ResolveBlob(ctx context.Context, repo string, digest ociregistry.Digest) (ociregistry.Descriptor, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return ociregistry.Descriptor{}, err
	}
	return cr.ResolveBlob(ctx, repo, digest)
}

func (r *registry) ResolveManifest(ctx context.Context, repo string, digest ociregistry.Digest) (ociregistry.Descriptor, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return ociregistry.Descriptor{}, err
	}
	return cr.ResolveManifest(ctx, repo, digest)
}

func (r *registry) ResolveTag(ctx context.Context, repo string, tagName string) (ociregistry.Descriptor, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return ociregistry.Descriptor{}, err
	}
	return cr.ResolveTag(ctx, repo, tagName)
}

func (r *registry) PushBlob(ctx context.Context, repo string, desc ociregistry.Descriptor, rd io.Reader) (ociregistry.Descriptor, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return ociregistry.Descriptor{}, err
	}
	return cr.PushBlob(ctx, repo, desc, rd)
}

func (r *registry) PushBlobChunked(ctx context.Context, repo string, chunkSize int) (ociregistry.BlobWriter, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return nil, err
	}
	return cr.PushBlobChunked(ctx, repo, chunkSize)
}

func (r *registry) PushBlobChunkedResume(ctx context.Context, repo, id string, offset int64, chunkSize int) (ociregistry.BlobWriter, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return nil, err
	}
	return cr.PushBlobChunkedResume(ctx, repo, id, offset, chunkSize)
}

func (r *registry) MountBlob(ctx context.Context, fromRepo, toRepo string, digest ociregistry.Digest) (ociregistry.Descriptor, error) {
	return ociregistry.Descriptor{}, ociregistry.ErrUnsupported
}

func (r *registry) PushManifest(ctx context.Context, repo string, tag string, contents []byte, mediaType string) (ociregistry.Descriptor, error) {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return ociregistry.Descriptor{}, err
	}
	return cr.PushManifest(ctx, repo, tag, contents, mediaType)
}

func (r *registry) DeleteBlob(ctx context.Context, repo string, digest ociregistry.Digest) error {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return err
	}
	return cr.DeleteBlob(ctx, repo, digest)
}

func (r *registry) DeleteManifest(ctx context.Context, repo string, digest ociregistry.Digest) error {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return err
	}
	return cr.DeleteManifest(ctx, repo, digest)
}

func (r *registry) DeleteTag(ctx context.Context, repo string, name string) error {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return err
	}
	return cr.DeleteTag(ctx, repo, name)
}

func (r *registry) Repositories(ctx context.Context) ociregistry.Iter[string] {
	return ociregistry.ErrorIter[string](ociregistry.ErrUnsupported)
}

func (r *registry) Tags(ctx context.Context, repo string) ociregistry.Iter[string] {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return ociregistry.ErrorIter[string](err)
	}
	return cr.Tags(ctx, repo)
}

func (r *registry) Referrers(ctx context.Context, repo string, digest ociregistry.Digest, artifactType string) ociregistry.Iter[ociregistry.Descriptor] {
	cr, repo, err := r.resolve(repo)
	if err != nil {
		return ociregistry.ErrorIter[ociregistry.Descriptor](err)
	}
	return cr.Referrers(ctx, repo, digest, artifactType)
}

func (r *registry) resolve(repo string) (reg ociregistry.Interface, repo1 string, err error) {
	loc := r.resolver.Resolve(repo)
	r.mu.Lock()
	defer r.mu.Unlock()
	reg = r.repos[loc.Host]
	if reg == nil {
		reg1, err := r.newRegistry(loc.Host, loc.Insecure)
		if err != nil {
			return nil, "", fmt.Errorf("cannot make client: %v", err)
		}
		r.repos[loc.Host] = reg1
		reg = reg1
	}
	return reg, join(loc.Prefix, repo), nil
}

// join is similar to path.Join but doesn't Clean the result, because
// that's not appropriate in this scenario.
func join(prefix, repo string) string {
	if prefix == "" {
		return repo
	}
	if repo == "" {
		return prefix
	}
	return prefix + "/" + repo
}
