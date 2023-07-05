package ociregistry

import (
	"context"
	"errors"
	"fmt"
	"io"

	"cuelabs.dev/go/oci/ociregistry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Registry implements one-level interface API to the oras registry API.
// It's defined like this so that the required API surface area is clear
// and it's easier to implement for tests.
type registry interface {
	Push(ctx context.Context, repoName string, desc ocispec.Descriptor, content io.Reader) error
	PushManifest(ctx context.Context, repoName string, tag string, content []byte, mediaType string) error
	Fetch(ctx context.Context, repoName string, desc ocispec.Descriptor) (io.ReadCloser, error)
	FetchManifest(ctx context.Context, repoName string, desc ocispec.Descriptor) (io.ReadCloser, error)
	Resolve(ctx context.Context, repoName string, tag string) (ocispec.Descriptor, error)
	Tags(ctx context.Context, repoName string) ([]string, error)
	Mount(ctx context.Context, fromRepo, toRepo string, desc ocispec.Descriptor) error
}

type registryShim struct {
	r ociregistry.Interface
}

func (r registryShim) Tags(ctx context.Context, repoName string) ([]string, error) {
	iter := r.r.Tags(ctx, repoName)
	defer iter.Close()
	var tags []string
	for {
		tag, ok := iter.Next()
		if !ok {
			break
		}
		tags = append(tags, tag)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("error reading tags: %v", err)
	}

	return tags, nil
}

func (r registryShim) Fetch(ctx context.Context, repoName string, desc ocispec.Descriptor) (io.ReadCloser, error) {
	return r.r.GetBlob(ctx, repoName, desc.Digest)
}

func (r registryShim) FetchManifest(ctx context.Context, repoName string, desc ocispec.Descriptor) (io.ReadCloser, error) {
	return r.r.GetManifest(ctx, repoName, desc.Digest)
}

func (r registryShim) Resolve(ctx context.Context, repoName string, tag string) (ocispec.Descriptor, error) {
	return r.r.ResolveTag(ctx, repoName, tag)
}

func (r registryShim) Push(ctx context.Context, repoName string, desc ocispec.Descriptor, content io.Reader) error {
	_, err := r.r.PushBlob(ctx, repoName, desc, content)
	return err
}

func (r registryShim) Mount(ctx context.Context, fromRepo, toRepo string, desc ocispec.Descriptor) error {
	_, err := r.r.MountBlob(ctx, fromRepo, toRepo, desc.Digest)
	if err == nil || !errors.Is(err, ociregistry.ErrUnsupported) {
		return err
	}
	// The registry doesn't support mounting. Try copying instead.
	rd, err := r.r.GetBlob(ctx, fromRepo, desc.Digest)
	if err != nil {
		return err
	}
	defer rd.Close()
	_, err = r.r.PushBlob(ctx, toRepo, desc, rd)
	return err
}

func (r registryShim) PushManifest(ctx context.Context, repoName string, tag string, content []byte, mediaType string) error {
	_, err := r.r.PushManifest(ctx, repoName, tag, content, mediaType)
	return err
}
