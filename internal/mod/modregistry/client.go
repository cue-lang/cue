// Copyright 2023 CUE Authors
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

package modregistry

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/mod/semver"

	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/module"
	modzip "cuelang.org/go/internal/mod/zip"
)

var ErrNotFound = fmt.Errorf("module not found")

// Client represents a OCI-registry-backed client that
// provides a store for CUE modules.
type Client struct {
	registry ociregistry.Interface
	prefix   string
}

const (
	moduleArtifactType  = "application/vnd.cue.module.v1+json"
	moduleFileMediaType = "application/vnd.cue.modulefile.v1"
	moduleAnnotation    = "works.cue.module"
)

// NewClient returns a new client that talks to the registry at the given
// hostname. All repositories created or accessed in the registry
// will have the given prefix.
//
// TODO pass in an ociregistry.Interface instead of a URL,
// thus allowing a locally defined composition of registries
// rather than always assuming everything is behind a single host.
func NewClient(registryURL string, prefix string) (*Client, error) {
	r := ociclient.New(registryURL, nil)
	return &Client{
		registry: r,
		prefix:   prefix,
	}, nil
}

// GetModule returns the module instance for the given version.
// It returns an error that satisfies errors.Is(ErrNotFound) if the
// module is not present in the store at this version.
func (c *Client) GetModule(ctx context.Context, m module.Version) (*Module, error) {
	repoName := c.repoName(m.Path())
	modDesc, err := c.registry.ResolveTag(ctx, repoName, m.Version())
	if err != nil {
		if errors.Is(err, ociregistry.ErrManifestUnknown) {
			return nil, fmt.Errorf("module %q: %w", m, ErrNotFound)
		}
		return nil, fmt.Errorf("module %v: %v", m, err)
	}
	manifest, err := fetchManifest(ctx, c.registry, repoName, modDesc)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal manifest data: %v", err)
	}
	if !isModule(manifest) {
		return nil, fmt.Errorf("%v does not resolve to a manifest (media type is %q)", m, modDesc.MediaType)
	}
	// TODO check type of manifest too.
	if n := len(manifest.Layers); n != 2 {
		return nil, fmt.Errorf("module manifest should refer to exactly two blobs, but got %d", n)
	}
	if !isModuleFile(manifest.Layers[1]) {
		return nil, fmt.Errorf("unexpected media type %q for module file blob", manifest.Layers[1].MediaType)
	}
	// TODO check that the other blobs are of the expected type (application/zip).
	return &Module{
		client:   c,
		repo:     repoName,
		manifest: *manifest,
	}, nil
}

func (c *Client) repoName(modPath string) string {
	path, _, _ := module.SplitPathVersion(modPath)
	return c.prefix + path
}

// ModuleVersions returns all the versions for the module with the given path.
func (c *Client) ModuleVersions(ctx context.Context, m string) ([]string, error) {
	_, major, ok := module.SplitPathVersion(m)
	if !ok {
		return nil, fmt.Errorf("non-canonical module path %q", m)
	}
	var tags []string
	iter := c.registry.Tags(ctx, c.repoName(m))
	for {
		tag, ok := iter.Next()
		if !ok {
			break
		}
		if semver.IsValid(tag) && semver.Major(tag) == major {
			tags = append(tags, tag)
		}
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return tags, nil
}

// PutModule puts a module whose contents are held as a zip archive inside f.
// It assumes all the module dependencies are correctly resolved and present
// inside the cue.mod/module.cue file.
//
// TODO check deps are resolved correctly? Or is that too domain-specific for this package?
// Is it a problem to call zip.CheckZip twice?
func (c *Client) PutModule(ctx context.Context, m module.Version, r io.ReaderAt, size int64) error {
	repoName := c.repoName(m.Path())
	_, modf, _, err := modzip.CheckZip(m, r, size)
	if err != nil {
		return fmt.Errorf("module zip file check failed: %v", err)
	}
	modFileContent, err := c.checkModFile(ctx, m, modf)
	if err != nil {
		return fmt.Errorf("module.cue file check failed: %v", err)
	}
	selfDigest, err := digest.FromReader(io.NewSectionReader(r, 0, size))
	if err != nil {
		return fmt.Errorf("cannot read module zip file: %v", err)
	}
	// Upload the actual module's content
	// TODO should we use a custom media type for this?
	selfDesc := ocispec.Descriptor{
		Digest:    selfDigest,
		MediaType: "application/zip",
		Size:      size,
	}
	configDesc, err := c.scratchConfig(ctx, repoName, moduleArtifactType)
	if err != nil {
		return fmt.Errorf("cannot make scratch config: %v", err)
	}
	manifest := &ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2, // historical value. does not pertain to OCI or docker version
		},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		// One for self, one for module file.
		Layers: make([]ocispec.Descriptor, 2),
	}
	manifest.Layers[0] = selfDesc

	manifest.Layers[1].Digest = digest.FromBytes(modFileContent)
	manifest.Layers[1].MediaType = moduleFileMediaType
	manifest.Layers[1].Size = int64(len(modFileContent))

	if _, err := c.registry.PushBlob(ctx, repoName, manifest.Layers[0], io.NewSectionReader(r, 0, size)); err != nil {
		return fmt.Errorf("cannot push module contents: %v", err)
	}
	if _, err := c.registry.PushBlob(ctx, repoName, manifest.Layers[1], bytes.NewReader(modFileContent)); err != nil {
		return fmt.Errorf("cannot push cue.mod/module.cue contents: %v", err)
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("cannot marshal manifest: %v", err)
	}
	if _, err := c.registry.PushManifest(ctx, repoName, m.Version(), manifestData, ocispec.MediaTypeImageManifest); err != nil {
		return fmt.Errorf("cannot tag %v: %v", m, err)
	}
	return nil
}

func (c *Client) checkModFile(ctx context.Context, m module.Version, f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// TODO check max size?
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	mf, err := modfile.Parse(data, f.Name)
	if err != nil {
		return nil, err
	}
	if mf.Module != m.Path() {
		return nil, fmt.Errorf("module path %q found in %s does not match module path being published %q", mf.Module, f.Name, m.Path())
	}
	_, major, ok := module.SplitPathVersion(mf.Module)
	if !ok {
		return nil, fmt.Errorf("invalid module path %q", mf.Module)
	}
	wantMajor := semver.Major(m.Version())
	if major != wantMajor {
		// This can't actually happen because the zip checker checks the major version
		// that's being published to, so the above path check also implicitly checks that.
		return nil, fmt.Errorf("major version %q found in %s does not match version being published %q", major, f.Name, m.Version())
	}
	// Check that all dependency versions look valid.
	for modPath, dep := range mf.Deps {
		_, err := module.NewVersion(modPath, dep.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid dependency: %v @ %v", modPath, dep.Version)
		}
	}
	return data, nil
}

// Module represents a CUE module instance.
type Module struct {
	client   *Client
	repo     string
	manifest ocispec.Manifest
}

// ModuleFile returns the contents of the cue.mod/module.cue file.
func (m *Module) ModuleFile(ctx context.Context) ([]byte, error) {
	r, err := m.client.registry.GetBlob(ctx, m.repo, m.manifest.Layers[1].Digest)
	if err != nil {
		return nil, err
	}
	return readAll(r)
}

// GetZip returns a reader that can be used to read the contents of the zip
// archive containing the module files. The reader should be closed after use,
// and the contents should not be assumed to be correct until the close
// error has been checked.
func (m *Module) GetZip(ctx context.Context) (io.ReadCloser, error) {
	return m.client.registry.GetBlob(ctx, m.repo, m.manifest.Layers[0].Digest)
}

func fetchManifest(ctx context.Context, r ociregistry.Interface, repoName string, desc ocispec.Descriptor) (*ociregistry.Manifest, error) {
	if !isJSON(desc.MediaType) {
		return nil, fmt.Errorf("expected JSON media type but %q does not look like JSON", desc.MediaType)
	}
	rd, err := r.GetManifest(ctx, repoName, desc.Digest)
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	data, err := readAll(rd)
	if err != nil {
		return nil, err
	}
	var m ociregistry.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("cannot decode %s content as manifest: %v", desc.MediaType, err)
	}
	return &m, nil
}

func readAll(r ociregistry.BlobReader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("cannot read content: %v", err)
	}
	if err := r.Close(); err != nil {
		return nil, err
	}
	return data, err
}

func isModule(m *ocispec.Manifest) bool {
	// TODO check m.ArtifactType too when that's defined?
	// See https://github.com/opencontainers/image-spec/blob/main/manifest.md#image-manifest-property-descriptions
	return m.Config.MediaType == moduleArtifactType
}

func isModuleFile(desc ocispec.Descriptor) bool {
	return desc.ArtifactType == moduleFileMediaType ||
		desc.MediaType == moduleFileMediaType
}

// isJSON reports whether the given media type has JSON as an underlying encoding.
// TODO this is a guess. There's probably a more correct way to do it.
func isJSON(mediaType string) bool {
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "/json")
}

// scratchConfig returns a dummy configuration consisting only of the
// two-byte configuration {}.
// https://github.com/opencontainers/image-spec/blob/main/manifest.md#example-of-a-scratch-config-or-layer-descriptor
func (c *Client) scratchConfig(ctx context.Context, repoName string, mediaType string) (ocispec.Descriptor, error) {
	// TODO check if it exists already to avoid push?
	content := []byte("{}")
	desc := ocispec.Descriptor{
		Digest:    digest.FromBytes(content),
		MediaType: mediaType,
		Size:      int64(len(content)),
	}
	if _, err := c.registry.PushBlob(ctx, repoName, desc, bytes.NewReader(content)); err != nil {
		return ocispec.Descriptor{}, err
	}
	return desc, nil
}
