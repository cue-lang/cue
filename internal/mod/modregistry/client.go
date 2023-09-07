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
	"io/ioutil"
	"sort"
	"strings"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelang.org/go/internal/mod/semver"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/module"
	modzip "cuelang.org/go/internal/mod/zip"
)

var ErrNotFound = fmt.Errorf("module not found")

// Client represents a OCI-registry-backed client that
// provides a store for CUE modules.
type Client struct {
	registry registry
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
		registry: registryShim{r},
		prefix:   prefix,
	}, nil
}

// GetModule returns the module instance for the given version.
// It returns an error that satisfies errors.Is(ErrNotFound) if the
// module is not present in the store at this version.
func (c *Client) GetModule(ctx context.Context, m module.Version) (*Module, error) {
	repoName := c.repoName(m.Path())
	modDesc, err := c.registry.Resolve(ctx, repoName, m.Version())
	if err != nil {
		if errors.Is(err, ociregistry.ErrManifestUnknown) {
			return nil, fmt.Errorf("module %q: %w", m, ErrNotFound)
		}
		return nil, fmt.Errorf("module %v: %v", m, err)
	}
	var manifest ocispec.Manifest
	if err := fetchJSON(ctx, c.registry.FetchManifest, repoName, modDesc, &manifest); err != nil {
		return nil, fmt.Errorf("cannot unmarshal manifest data: %v", err)
	}
	if !isModule(&manifest) {
		return nil, fmt.Errorf("%v does not resolve to a manifest (media type is %q)", m, modDesc.MediaType)
	}
	// TODO check type of manifest too.
	if n := len(manifest.Layers); n < 2 {
		return nil, fmt.Errorf("not enough blobs found in module manifest; need at least 2, got %d", n)
	}
	if !isModuleFile(manifest.Layers[1]) {
		return nil, fmt.Errorf("unexpected media type %q for module file blob", manifest.Layers[1].MediaType)
	}
	// TODO check that all other blobs are of the expected type (application/zip)
	// and that dependencies have the expected attribute.
	return &Module{
		client:   c,
		repo:     repoName,
		manifest: manifest,
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
	tags, err := c.registry.Tags(ctx, c.repoName(m))
	if err != nil {
		return nil, err
	}
	j := 0
	for _, tag := range tags {
		if semver.IsValid(tag) && semver.Major(tag) == major {
			tags[j] = tag
			j++
		}
	}
	return tags[:j], nil
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
	modFileContent, deps, err := c.checkModFile(ctx, m, modf)
	if err != nil {
		return fmt.Errorf("module.cue file check failed: %v", err)
	}
	selfDigest, err := digest.FromReader(io.NewSectionReader(r, 0, size))
	if err != nil {
		return fmt.Errorf("cannot read module zip file: %v", err)
	}

	if err := c.uploadDeps(ctx, m, deps); err != nil {
		return fmt.Errorf("cannot upload dependencies: %v", err)
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
		// One for self, one for module file + 1 for each dependency.
		Layers: make([]ocispec.Descriptor, 2+len(deps)),
	}
	manifest.Layers[0] = selfDesc

	manifest.Layers[1].Digest = digest.FromBytes(modFileContent)
	manifest.Layers[1].MediaType = moduleFileMediaType
	manifest.Layers[1].Size = int64(len(modFileContent))

	//log.Printf("push layer 0 %s@%s %s", repoName, manifest.Layers[0].Digest, manifest.Layers[0].MediaType)
	if err := c.registry.Push(ctx, repoName, manifest.Layers[0], io.NewSectionReader(r, 0, size)); err != nil {
		return fmt.Errorf("cannot push module contents: %v", err)
	}
	//log.Printf("push layer 1 %s@%s %s", repoName, manifest.Layers[1].Digest, manifest.Layers[1].MediaType)
	if err := c.registry.Push(ctx, repoName, manifest.Layers[1], bytes.NewReader(modFileContent)); err != nil {
		return fmt.Errorf("cannot push cue.mod/module.cue contents: %v", err)
	}

	depVersions := mapKeys(deps)
	// Create the layers in predictable order.
	sort.Slice(depVersions, func(i, j int) bool {
		v1, v2 := depVersions[i], depVersions[j]
		if v1.Path() == v2.Path() {
			// This can't happen currently but probably could in the future
			// when we keep one version for main module and a different
			// for when used as dependency.
			return semver.Compare(v1.Version(), v2.Version()) < 0
		}
		return v1.Path() < v2.Path()
	})
	for i, v := range depVersions {
		layer := &manifest.Layers[i+2]
		*layer = deps[v].manifest.Layers[0]
		layer.Annotations = map[string]string{
			"works.cue.module": v.String(),
		}
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("cannot marshal manifest: %v", err)
	}
	if err := c.registry.PushManifest(ctx, repoName, m.Version(), manifestData, ocispec.MediaTypeImageManifest); err != nil {
		return fmt.Errorf("cannot tag %v: %v", m, err)
	}
	return nil
}

func (c *Client) uploadDeps(ctx context.Context, dst module.Version, deps map[module.Version]*Module) error {
	// Since we've verified that all the dependencies exist, we can just
	// mount them directly from where they were originally.

	dstRepo := c.repoName(dst.Path())
	// TODO do this concurrently
	for src, m := range deps {
		//log.Printf("mount %v@%v %v", m.repo, m.manifest.Layers[0].Digest, m.repo)
		if err := c.registry.Mount(ctx, m.repo, dstRepo, m.manifest.Layers[0]); err != nil {
			return fmt.Errorf("failed to make %v available as dependency in %v: %v", src, dst.Path(), err)
		}
	}
	return nil
}

func (c *Client) checkModFile(ctx context.Context, m module.Version, f *zip.File) ([]byte, map[module.Version]*Module, error) {
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
	if mf.Module != m.Path() {
		return nil, nil, fmt.Errorf("module path %q found in %s does not match module path being published %q", mf.Module, f.Name, m.Path())
	}
	_, major, ok := module.SplitPathVersion(mf.Module)
	if !ok {
		return nil, nil, fmt.Errorf("invalid module path %q", mf.Module)
	}
	wantMajor := semver.Major(m.Version())
	if major != wantMajor {
		// This can't actually happen because the zip checker checks the major version
		// that's being published to, so the above path check also implicitly checks that.
		return nil, nil, fmt.Errorf("major version %q found in %s does not match version being published %q", major, f.Name, m.Version())
	}
	deps := make(map[module.Version]*Module)
	for modPath, dep := range mf.Deps {
		depv, err := module.NewVersion(modPath, dep.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid dependency: %v @ %v", modPath, dep.Version)
		}
		dm, err := c.GetModule(ctx, depv)
		if err != nil {
			// TODO try getting all modules so we get told about all missing dependencies rather than just one?
			return nil, nil, fmt.Errorf("cannot get module %v: %v", depv, err)
		}
		deps[depv] = dm
	}
	// TODO run MVS on the module file and check that
	// the versions in the module dependencies reflect the final versions.
	return data, deps, nil
}

// Module represents a CUE module instance.
type Module struct {
	client   *Client
	repo     string
	manifest ocispec.Manifest
}

// ModuleFile returns the contents of the cue.mod/module.cue file.
func (m *Module) ModuleFile(ctx context.Context) ([]byte, error) {
	return fetchBytes(ctx, m.client.registry.Fetch, m.repo, m.manifest.Layers[1])
}

// GetZip returns a reader that can be used to read the contents of the zip
// archive containing the module files. The reader should be closed after use,
// and the contents should not be assumed to be correct until the close
// error has been checked.
func (m *Module) GetZip(ctx context.Context) (io.ReadCloser, error) {
	return m.client.registry.Fetch(ctx, m.repo, m.manifest.Layers[0])
}

// Dependencies returns all the module's dependencies available inside the module.
func (m *Module) Dependencies(ctx context.Context) (map[module.Version]Dependency, error) {
	deps := make(map[module.Version]Dependency)
	for _, desc := range m.manifest.Layers[2:] {
		mname, ok := desc.Annotations[moduleAnnotation]
		if !ok {
			return nil, fmt.Errorf("no %s annotation found for blob", moduleAnnotation)
		}
		mv, err := module.ParseVersion(mname)
		if err != nil {
			return nil, fmt.Errorf("bad module name %q found in module config", mname)
		}
		deps[mv] = Dependency{
			client:  m.client,
			version: mv,
			desc:    desc,
		}
	}
	return deps, nil
}

// Dependency represents a module depended on by another module.
type Dependency struct {
	client  *Client
	version module.Version
	desc    ocispec.Descriptor
}

// Version returns the version of the dependency.
func (d Dependency) Version() module.Version {
	return d.version
}

// GetZip returns a reader for the dependency's archive.
func (d Dependency) GetZip() (io.ReadCloser, error) {
	panic("unimplemented")
}

type fetchFunc = func(ctx context.Context, repoName string, desc ocispec.Descriptor) (io.ReadCloser, error)

func fetchJSON(ctx context.Context, fetch fetchFunc, repoName string, desc ocispec.Descriptor, dst any) error {
	if !isJSON(desc.MediaType) {
		return fmt.Errorf("expected JSON media type but %q does not look like JSON", desc.MediaType)
	}
	data, err := fetchBytes(ctx, fetch, repoName, desc)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("cannot decode %s content into %T: %v", desc.MediaType, dst, err)
	}
	return nil
}

func fetchBytes(ctx context.Context, fetch fetchFunc, repoName string, desc ocispec.Descriptor) ([]byte, error) {
	r, err := fetch(ctx, repoName, desc)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch content: %v", err)
	}
	defer r.Close()
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("cannot read content: %v", err)
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

func mapKeys[K comparable, V any](m map[K]V) []K {
	ks := make([]K, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
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
	if err := c.registry.Push(ctx, repoName, desc, bytes.NewReader(content)); err != nil {
		return ocispec.Descriptor{}, err
	}
	return desc, nil
}
