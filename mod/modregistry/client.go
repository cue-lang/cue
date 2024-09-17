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

// Package modregistry provides functionality for reading and writing
// CUE modules from an OCI registry.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package modregistry

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociref"
	"cuelang.org/go/internal/mod/semver"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

var ErrNotFound = fmt.Errorf("module not found")

// Client represents a OCI-registry-backed client that
// provides a store for CUE modules.
type Client struct {
	resolver Resolver
}

// Resolver resolves module paths to a registry and a location
// within that registry.
type Resolver interface {
	// ResolveToRegistry resolves a base module path (without a version)
	// and optional version to the location for that path.
	//
	// If the version is empty, the Tag in the returned Location
	// will hold the prefix that all versions of the module in its
	// repository have. That prefix will be followed by the version
	// itself.
	//
	// If there is no registry configured for the module, it returns
	// an [ErrRegistryNotFound] error.
	ResolveToRegistry(mpath, vers string) (RegistryLocation, error)
}

// ErrRegistryNotFound is returned by [Resolver.ResolveToRegistry]
// when there is no registry configured for a module.
var ErrRegistryNotFound = fmt.Errorf("no registry configured for module")

// RegistryLocation holds a registry and a location within it
// that a specific module (or set of versions for a module)
// will be stored.
type RegistryLocation struct {
	// Registry holds the registry to use to access the module.
	Registry ociregistry.Interface
	// Repository holds the repository where the module is located.
	Repository string
	// Tag holds the tag for the module version. If an empty version
	// was passed to Resolve, it holds the prefix shared by all
	// version tags for the module.
	Tag string
}

const (
	moduleArtifactType  = "application/vnd.cue.module.v1+json"
	moduleFileMediaType = "application/vnd.cue.modulefile.v1"
	moduleAnnotation    = "works.cue.module"
)

// NewClient returns a new client that talks to the registry at the given
// hostname.
func NewClient(registry ociregistry.Interface) *Client {
	return &Client{
		resolver: singleResolver{registry},
	}
}

// NewClientWithResolver returns a new client that uses the given
// resolver to decide which registries to fetch from or push to.
func NewClientWithResolver(resolver Resolver) *Client {
	return &Client{
		resolver: resolver,
	}
}

// GetModule returns the module instance for the given version.
// It returns an error that satisfies [errors.Is]([ErrNotFound]) if the
// module is not present in the store at this version.
func (c *Client) GetModule(ctx context.Context, m module.Version) (*Module, error) {
	loc, err := c.resolve(m)
	if err != nil {
		if errors.Is(err, ErrRegistryNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rd, err := loc.Registry.GetTag(ctx, loc.Repository, loc.Tag)
	if err != nil {
		if isNotExist(err) {
			return nil, fmt.Errorf("module %v: %w", m, ErrNotFound)
		}
		return nil, fmt.Errorf("module %v: %w", m, err)
	}
	defer rd.Close()
	data, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}

	return c.GetModuleWithManifest(m, data, rd.Descriptor().MediaType)
}

// GetModuleWithManifest returns a module instance given
// the top level manifest contents, without querying its tag.
// It assumes that the module will be tagged with the given version.
func (c *Client) GetModuleWithManifest(m module.Version, contents []byte, mediaType string) (*Module, error) {
	loc, err := c.resolve(m)
	if err != nil {
		// Note: don't return [ErrNotFound] here because if we've got the
		// manifest we should be pretty sure that the module actually
		// exists, so it's a harder error than if we're getting the module
		// by tag.
		return nil, err
	}

	manifest, err := unmarshalManifest(contents, mediaType)
	if err != nil {
		return nil, fmt.Errorf("module %v: %v", m, err)
	}
	if !isModule(manifest) {
		return nil, fmt.Errorf("%v does not resolve to a manifest (media type is %q)", m, mediaType)
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
		client:         c,
		loc:            loc,
		version:        m,
		manifest:       *manifest,
		manifestDigest: digest.FromBytes(contents),
	}, nil
}

// ModuleVersions returns all the versions for the module with the given path
// sorted in semver order.
// If m has a major version suffix, only versions with that major version will
// be returned.
func (c *Client) ModuleVersions(ctx context.Context, m string) (_req []string, _err0 error) {
	mpath, major, hasMajor := module.SplitPathVersion(m)
	if !hasMajor {
		mpath = m
	}
	loc, err := c.resolver.ResolveToRegistry(mpath, "")
	if err != nil {
		if errors.Is(err, ErrRegistryNotFound) {
			return nil, nil
		}
		return nil, err
	}
	versions := []string{}
	if !ociref.IsValidRepository(loc.Repository) {
		// If it's not a valid repository, it can't be used in an OCI
		// request, so return an empty slice rather than the
		// "invalid OCI request" error that a registry can return.
		return nil, nil
	}
	// Note: do not use c.repoName because that always expects
	// a module path with a major version.
	iter := loc.Registry.Tags(ctx, loc.Repository, "")
	var _err error
	iter(func(tag string, err error) bool {
		if err != nil {
			_err = err
			return false
		}
		vers, ok := strings.CutPrefix(tag, loc.Tag)
		if !ok || !semver.IsValid(vers) {
			return true
		}
		if !hasMajor || semver.Major(vers) == major {
			versions = append(versions, vers)
		}
		return true
	})
	if _err != nil && !isNotExist(_err) {
		return nil, fmt.Errorf("module %v: %w", m, _err)
	}
	semver.Sort(versions)
	return versions, nil
}

// checkedModule represents module content that has passed the same
// checks made by [Client.PutModule]. The caller should not mutate
// any of the values returned by its methods.
type checkedModule struct {
	mv             module.Version
	blobr          io.ReaderAt
	size           int64
	zipr           *zip.Reader
	modFile        *modfile.File
	modFileContent []byte
}

// putCheckedModule is like [Client.PutModule] except that it allows the
// caller to do some additional checks (see [CheckModule] for more info).
func (c *Client) putCheckedModule(ctx context.Context, m *checkedModule, meta *Metadata) error {
	var annotations map[string]string
	if meta != nil {
		annotations0, err := meta.annotations()
		if err != nil {
			return fmt.Errorf("invalid metadata: %v", err)
		}
		annotations = annotations0
	}
	loc, err := c.resolve(m.mv)
	if err != nil {
		return err
	}
	selfDigest, err := digest.FromReader(io.NewSectionReader(m.blobr, 0, m.size))
	if err != nil {
		return fmt.Errorf("cannot read module zip file: %v", err)
	}
	// Upload the actual module's content
	// TODO should we use a custom media type for this?
	configDesc, err := c.scratchConfig(ctx, loc, moduleArtifactType)
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
		Layers: []ocispec.Descriptor{{
			Digest:    selfDigest,
			MediaType: "application/zip",
			Size:      m.size,
		}, {
			Digest:    digest.FromBytes(m.modFileContent),
			MediaType: moduleFileMediaType,
			Size:      int64(len(m.modFileContent)),
		}},
		Annotations: annotations,
	}

	if _, err := loc.Registry.PushBlob(ctx, loc.Repository, manifest.Layers[0], io.NewSectionReader(m.blobr, 0, m.size)); err != nil {
		return fmt.Errorf("cannot push module contents: %v", err)
	}
	if _, err := loc.Registry.PushBlob(ctx, loc.Repository, manifest.Layers[1], bytes.NewReader(m.modFileContent)); err != nil {
		return fmt.Errorf("cannot push cue.mod/module.cue contents: %v", err)
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("cannot marshal manifest: %v", err)
	}
	if _, err := loc.Registry.PushManifest(ctx, loc.Repository, loc.Tag, manifestData, ocispec.MediaTypeImageManifest); err != nil {
		return fmt.Errorf("cannot tag %v: %v", m.mv, err)
	}
	return nil
}

// PutModule puts a module whose contents are held as a zip archive inside f.
// It assumes all the module dependencies are correctly resolved and present
// inside the cue.mod/module.cue file.
func (c *Client) PutModule(ctx context.Context, m module.Version, r io.ReaderAt, size int64) error {
	return c.PutModuleWithMetadata(ctx, m, r, size, nil)
}

// PutModuleWithMetadata is like [Client.PutModule] except that it also
// includes the given metadata inside the module's manifest.
// If meta is nil, no metadata will be included, otherwise
// all fields in meta must be valid and non-empty.
func (c *Client) PutModuleWithMetadata(ctx context.Context, m module.Version, r io.ReaderAt, size int64, meta *Metadata) error {
	cm, err := checkModule(m, r, size)
	if err != nil {
		return err
	}
	return c.putCheckedModule(ctx, cm, meta)
}

// checkModule checks a module's zip file before uploading it.
// This does the same checks that [Client.PutModule] does, so
// can be used to avoid doing duplicate work when an uploader
// wishes to do more checks that are implemented by that method.
//
// Note that the returned [CheckedModule] value contains r, so will
// be invalidated if r is closed.
func checkModule(m module.Version, blobr io.ReaderAt, size int64) (*checkedModule, error) {
	zipr, modf, _, err := modzip.CheckZip(m, blobr, size)
	if err != nil {
		return nil, fmt.Errorf("module zip file check failed: %v", err)
	}
	modFileContent, mf, err := checkModFile(m, modf)
	if err != nil {
		return nil, fmt.Errorf("module.cue file check failed: %v", err)
	}
	return &checkedModule{
		mv:             m,
		blobr:          blobr,
		size:           size,
		zipr:           zipr,
		modFile:        mf,
		modFileContent: modFileContent,
	}, nil
}

func checkModFile(m module.Version, f *zip.File) ([]byte, *modfile.File, error) {
	r, err := f.Open()
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	// TODO check max size?
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	mf, err := modfile.Parse(data, f.Name)
	if err != nil {
		return nil, nil, err
	}
	if mf.QualifiedModule() != m.Path() {
		return nil, nil, fmt.Errorf("module path %q found in %s does not match module path being published %q", mf.QualifiedModule(), f.Name, m.Path())
	}
	wantMajor := semver.Major(m.Version())
	if major := mf.MajorVersion(); major != wantMajor {
		// This can't actually happen because the zip checker checks the major version
		// that's being published to, so the above path check also implicitly checks that.
		return nil, nil, fmt.Errorf("major version %q found in %s does not match version being published %q", major, f.Name, m.Version())
	}
	// Check that all dependency versions look valid.
	for modPath, dep := range mf.Deps {
		_, err := module.NewVersion(modPath, dep.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid dependency: %v @ %v", modPath, dep.Version)
		}
	}
	return data, mf, nil
}

// Module represents a CUE module instance.
type Module struct {
	client         *Client
	loc            RegistryLocation
	version        module.Version
	manifest       ocispec.Manifest
	manifestDigest ociregistry.Digest
}

func (m *Module) Version() module.Version {
	return m.version
}

// ModuleFile returns the contents of the cue.mod/module.cue file.
func (m *Module) ModuleFile(ctx context.Context) ([]byte, error) {
	r, err := m.loc.Registry.GetBlob(ctx, m.loc.Repository, m.manifest.Layers[1].Digest)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// Metadata returns the metadata associated with the module.
// If there is none, it returns (nil, nil).
func (m *Module) Metadata() (*Metadata, error) {
	return newMetadataFromAnnotations(m.manifest.Annotations)
}

// GetZip returns a reader that can be used to read the contents of the zip
// archive containing the module files. The reader should be closed after use,
// and the contents should not be assumed to be correct until the close
// error has been checked.
func (m *Module) GetZip(ctx context.Context) (io.ReadCloser, error) {
	return m.loc.Registry.GetBlob(ctx, m.loc.Repository, m.manifest.Layers[0].Digest)
}

// ManifestDigest returns the digest of the manifest representing
// the module.
func (m *Module) ManifestDigest() ociregistry.Digest {
	return m.manifestDigest
}

func (c *Client) resolve(m module.Version) (RegistryLocation, error) {
	loc, err := c.resolver.ResolveToRegistry(m.BasePath(), m.Version())
	if err != nil {
		return RegistryLocation{}, err
	}
	if loc.Registry == nil {
		return RegistryLocation{}, fmt.Errorf("module %v unexpectedly resolved to nil registry", m)
	}
	if loc.Repository == "" {
		return RegistryLocation{}, fmt.Errorf("module %v unexpectedly resolved to empty location", m)
	}
	if loc.Tag == "" {
		return RegistryLocation{}, fmt.Errorf("module %v unexpectedly resolved to empty tag", m)
	}
	return loc, nil
}

func unmarshalManifest(data []byte, mediaType string) (*ociregistry.Manifest, error) {
	if !isJSON(mediaType) {
		return nil, fmt.Errorf("expected JSON media type but %q does not look like JSON", mediaType)
	}
	var m ociregistry.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("cannot decode %s content as manifest: %v", mediaType, err)
	}
	return &m, nil
}

func isNotExist(err error) bool {
	if errors.Is(err, ociregistry.ErrNameUnknown) ||
		errors.Is(err, ociregistry.ErrNameInvalid) {
		return true
	}
	// A 403 error might have been sent as a response
	// without explicitly including a "denied" error code.
	// We treat this as a "not found" error because there's
	// nothing the user can do about it.
	//
	// Also, some registries return an invalid error code with a 404
	// response (see https://cuelang.org/issue/2982), so it
	// seems reasonable to treat that as a non-found error too.
	if herr := ociregistry.HTTPError(nil); errors.As(err, &herr) {
		statusCode := herr.StatusCode()
		return statusCode == http.StatusForbidden ||
			statusCode == http.StatusNotFound
	}
	return false
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
func (c *Client) scratchConfig(ctx context.Context, loc RegistryLocation, mediaType string) (ocispec.Descriptor, error) {
	// TODO check if it exists already to avoid push?
	content := []byte("{}")
	desc := ocispec.Descriptor{
		Digest:    digest.FromBytes(content),
		MediaType: mediaType,
		Size:      int64(len(content)),
	}
	if _, err := loc.Registry.PushBlob(ctx, loc.Repository, desc, bytes.NewReader(content)); err != nil {
		return ocispec.Descriptor{}, err
	}
	return desc, nil
}

// singleResolver implements Resolver by always returning R,
// and mapping module paths directly to repository paths in
// the registry.
type singleResolver struct {
	R ociregistry.Interface
}

func (r singleResolver) ResolveToRegistry(mpath, vers string) (RegistryLocation, error) {
	return RegistryLocation{
		Registry:   r.R,
		Repository: mpath,
		Tag:        vers,
	}, nil
}
