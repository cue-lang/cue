// Copyright 2023 The CUE Authors
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

package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociref"
	"github.com/opencontainers/go-digest"
	ocispecroot "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"cuelang.org/go/internal/vcs"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

func newModUploadCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <version>",
		Short: "publish the current module to a registry",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Publish the current module to an OCI registry. It consults
$CUE_REGISTRY to determine where the module should be published (see
"cue help environment" for details). Also note that this command does
no dependency or other checks at the moment.

Note: you must enable the modules experiment with:
	export CUE_EXPERIMENT=modules
for this command to work.
`,
		RunE: mkRunE(c, runModUpload),
		Args: cobra.ExactArgs(1),
	}
	cmd.Flags().BoolP(string(flagDryrun), "n", false, "only run simulation")
	cmd.Flags().Bool(string(flagJSON), false, "print verbose information in JSON format (implies --dryrun)")
	cmd.Flags().StringP(string(flagOut), "o", "", "write all module contents to specified directory in OCI Image Layout format (implies --dryrun)")

	return cmd
}

type publishInfo struct {
	Version  string   `json:"version"`
	Ref      string   `json:"ref"`
	Insecure bool     `json:"insecure,omitempty"`
	Files    []string `json:"files"`
	// TODO include metadata too.
}

func runModUpload(cmd *Command, args []string) error {
	ctx := cmd.Context()
	resolver0, err := getRegistryResolver()
	if err != nil {
		return err
	}
	if resolver0 == nil {
		return fmt.Errorf("modules experiment not enabled (enable with CUE_EXPERIMENT=modules)")
	}
	dryRun := flagDryrun.Bool(cmd)
	outDir := flagOut.String(cmd)
	useJSON := flagJSON.Bool(cmd)
	if outDir != "" || useJSON {
		dryRun = true
	}
	resolver := &publishRegistryResolverShim{
		resolver: resolver0,
		outDir:   flagOut.String(cmd),
		dryRun:   dryRun,
		// recording the files is somewhat heavyweight, so only do it
		// if we're going to need them.
		recordFiles: useJSON,
	}
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	// TODO ensure module tidiness.
	modPath := filepath.Join(modRoot, "cue.mod/module.cue")
	modfileData, err := os.ReadFile(modPath)
	if err != nil {
		return err
	}
	mf, err := modfile.Parse(modfileData, modPath)
	if err != nil {
		return err
	}
	mv, err := module.NewVersion(mf.Module, args[0])
	if err != nil {
		return fmt.Errorf("cannot form module version: %v", err)
	}
	if mf.Source == nil {
		// TODO print filename relative to current directory
		return fmt.Errorf("no source field found in cue.mod/module.cue")
	}
	zf, err := os.CreateTemp("", "cue-publish-")
	if err != nil {
		return err
	}
	defer os.Remove(zf.Name())
	defer zf.Close()

	// TODO verify that all dependencies exist in the registry.

	var meta *modregistry.Metadata

	switch mf.Source.Kind {
	case "self":
		if err := modzip.CreateFromDir(zf, mv, modRoot); err != nil {
			return err
		}
	default:
		vcsImpl, err := vcs.New(mf.Source.Kind, modRoot)
		if err != nil {
			return err
		}
		status, err := vcsImpl.Status(ctx)
		if err != nil {
			return err
		}
		if status.Uncommitted {
			// TODO implement --force to bypass this check
			return fmt.Errorf("VCS state is not clean")
		}
		files, err := vcsImpl.ListFiles(ctx, modRoot)
		if err != nil {
			return err
		}
		if err := modzip.Create[string](zf, mv, files, osFileIO{
			modRoot: modRoot,
		}); err != nil {
			return err
		}
		meta = &modregistry.Metadata{
			VCSType:       mf.Source.Kind,
			VCSCommit:     status.Revision,
			VCSCommitTime: status.CommitTime,
		}
	}
	info, err := zf.Stat()
	if err != nil {
		return err
	}

	rclient := modregistry.NewClientWithResolver(resolver)
	if err := rclient.PutModuleWithMetadata(backgroundContext(), mv, zf, info.Size(), meta); err != nil {
		return fmt.Errorf("cannot put module: %v", err)
	}
	log.Printf("host %q; repository %q; tag %q; digest: %v", resolver.registryName, resolver.repository, resolver.tag, resolver.manifestDigest)
	ref := ociref.Reference{
		Host:       resolver.registryName,
		Repository: resolver.repository,
		Tag:        resolver.tag,
		Digest:     resolver.manifestDigest,
	}
	if outDir != "" {
		if err := resolver.writeIndex(); err != nil {
			return err
		}
	}
	switch {
	case useJSON:
		info := publishInfo{
			Version:  mv.String(),
			Ref:      ref.String(),
			Insecure: resolver.insecure,
			Files:    resolver.files,
		}
		data, err := json.MarshalIndent(info, "", "\t")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		os.Stdout.Write(data)
	case outDir != "":
		fmt.Printf("wrote image for %s to %s\n", mv, outDir)
	case dryRun:
		fmt.Printf("dry-run published %s to %v\n", mv, ref)
	default:
		fmt.Printf("published %s to %v\n", mv, ref)
	}
	return nil
}

// osFileIO implements [modzip.FileIO] for filepath paths relative to
// the module root directory, as returned by [vcs.VCS.ListFiles].
type osFileIO struct {
	modRoot string
}

func (osFileIO) Path(f string) string {
	return filepath.ToSlash(f)
}

func (fio osFileIO) Lstat(f string) (fs.FileInfo, error) {
	return os.Lstat(fio.absPath(f))
}

func (fio osFileIO) Open(f string) (io.ReadCloser, error) {
	return os.Open(fio.absPath(f))
}

func (fio osFileIO) absPath(f string) string {
	return filepath.Join(fio.modRoot, f)
}

// publishRegistryResolverShim implements a wrapper around modregistry.Resolver
// that records information about the module being published.
//
// If dryRun is true, it does not actually write to the underlying
// registry.
//
// If outDir is non-empty, it also writes the contents of the module
// to that directory.
//
// If recordFiles is true, it records which files are present in the
// module's zip file.
type publishRegistryResolverShim struct {
	resolver    *modconfig.Resolver
	registry    ociregistry.Interface
	dryRun      bool
	recordFiles bool
	outDir      string

	initDirOnce  sync.Once
	initDirError error

	mu             sync.Mutex
	registryName   string
	insecure       bool
	repository     string
	tag            string
	manifestDigest digest.Digest
	files          []string
	descriptors    []ociregistry.Descriptor
}

// ResolveToRegistry implements [modregistry.RegistryResolver].
func (r *publishRegistryResolverShim) ResolveToRegistry(mpath, vers string) (modregistry.RegistryLocation, error) {
	// Make sure that we can acquire the underlying registry even
	// though we are not going to use it, so we're dry-running as
	// much as possible
	regLoc, err := r.resolver.ResolveToRegistry(mpath, vers)
	if err != nil {
		return modregistry.RegistryLocation{}, err
	}
	loc, ok := r.resolver.ResolveToLocation(mpath, vers)
	if !ok {
		panic("unreachable: ResolveToLocation failed when ResolveToRegistry succeeded")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registryName = loc.Host
	r.insecure = loc.Insecure
	return modregistry.RegistryLocation{
		Registry: &publishRegistryShim{
			Funcs: &ociregistry.Funcs{
				NewError: func(ctx context.Context, methodName, repo string) error {
					return fmt.Errorf("unexpected OCI method %q invoked when publishing module", methodName)
				},
			},
			resolver: r,
			registry: regLoc.Registry,
		},
		Repository: loc.Repository,
		Tag:        loc.Tag,
	}, nil
}

func (r *publishRegistryResolverShim) writeIndex() error {
	if r.outDir == "" {
		return nil
	}
	index := ocispec.Index{
		Versioned: ocispecroot.Versioned{
			SchemaVersion: 2,
		},
		MediaType: "application/vnd.oci.image.index.v1+json",
		Manifests: r.descriptors,
	}
	data, err := json.MarshalIndent(index, "", "\t")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if os.WriteFile(filepath.Join(r.outDir, "index.json"), data, 0o666); err != nil {
		return err
	}
	return nil
}

func (r *publishRegistryResolverShim) setRepository(repo string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.repository != "" && repo != r.repository {
		return fmt.Errorf("internal error: publish wrote to more than one OCI repository")
	}
	r.repository = repo
	return nil
}

func (r *publishRegistryResolverShim) setTag(tag string, dig digest.Digest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tag != "" && tag != r.tag {
		return fmt.Errorf("internal error: publish wrote to more than one OCI tag")
	}
	r.tag = tag
	r.manifestDigest = dig
	return nil
}

func (r *publishRegistryResolverShim) setFiles(files []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files = files
}

func (r *publishRegistryResolverShim) addManifest(tag string, data []byte, mediaType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.descriptors = append(r.descriptors, ociregistry.Descriptor{
		MediaType: mediaType,
		Size:      int64(len(data)),
		Digest:    digest.FromBytes(data),
		Annotations: map[string]string{
			"org.opencontainers.image.ref.name": tag,
		},
	})
}

func (r *publishRegistryResolverShim) initDir() error {
	// Lazily create the directory and the oci-layout file
	// so we don't create anything if no operations take place
	// on the registry.
	r.initDirOnce.Do(func() {
		if r.outDir == "" {
			return
		}
		if err := os.Mkdir(r.outDir, 0o777); err != nil {
			r.initDirError = err
			return
		}
		// Create oci-layout file.
		// See https://github.com/opencontainers/image-spec/blob/8f3820ccf8f65db8744e626df17fe8a64462aece/image-layout.md#oci-layout-file
		r.initDirError = os.WriteFile(filepath.Join(r.outDir, "oci-layout"), []byte(`{
    "imageLayoutVersion": "1.0.0"
}
`), 0o666)
	})
	return r.initDirError
}

// publishRegistryShim implements [ociregistry.Interface] by recording
// what is written. It returns an error for methods not expected to be
// invoked as part of the publishing process.
type publishRegistryShim struct {
	*ociregistry.Funcs
	resolver *publishRegistryResolverShim
	// registry is the real underlying registry. It is only used if
	// resolver.dryRun is false.
	registry ociregistry.Interface
}

func filesFromZip(content io.Reader) (io.Reader, []string, error) {
	// TODO ideally we wouldn't need to read the entire
	// zip file into memory to do this.
	data, err := io.ReadAll(content)
	if err != nil {
		return nil, nil, err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, err
	}
	files := make([]string, len(z.File))
	for i, f := range z.File {
		files[i] = f.Name
	}
	return bytes.NewReader(data), files, nil
}

func (r *publishRegistryShim) PushBlob(ctx context.Context, repoName string, desc ociregistry.Descriptor, content io.Reader) (ociregistry.Descriptor, error) {
	if err := r.resolver.setRepository(repoName); err != nil {
		return ociregistry.Descriptor{}, err
	}
	if r.resolver.recordFiles && desc.MediaType == "application/zip" {
		content1, files, err := filesFromZip(content)
		if err != nil {
			return ociregistry.Descriptor{}, err
		}
		content = content1
		r.resolver.setFiles(files)
	}

	switch {
	case r.resolver.outDir != "":
		if err := r.resolver.initDir(); err != nil {
			return ociregistry.Descriptor{}, err
		}
		outFile := filepath.Join(r.resolver.outDir, "blobs", string(desc.Digest.Algorithm()), desc.Digest.Encoded())
		if err := os.MkdirAll(filepath.Dir(outFile), 0o777); err != nil {
			return ociregistry.Descriptor{}, err
		}
		// TODO create as temp and atomically rename?
		f, err := os.Create(outFile)
		if err != nil {
			return ociregistry.Descriptor{}, err
		}
		defer f.Close()
		if _, err := io.Copy(f, content); err != nil {
			return ociregistry.Descriptor{}, fmt.Errorf("cannot copy blob to %q: %v", outFile, err)
		}
	case r.resolver.dryRun:
		// Sanity check we can read the content.
		if _, err := io.Copy(io.Discard, content); err != nil {
			return ociregistry.Descriptor{}, fmt.Errorf("error reading blob: %v", err)
		}
	default:
		return r.registry.PushBlob(ctx, repoName, desc, content)
	}
	return desc, nil
}

func (r *publishRegistryShim) PushManifest(ctx context.Context, repoName string, tag string, data []byte, mediaType string) (ociregistry.Descriptor, error) {
	if err := r.resolver.setRepository(repoName); err != nil {
		return ociregistry.Descriptor{}, err
	}
	desc := ociregistry.Descriptor{
		Digest: digest.FromBytes(data),
		Size:   int64(len(data)),
	}
	if err := r.resolver.setTag(tag, desc.Digest); err != nil {
		return ociregistry.Descriptor{}, err
	}
	r.resolver.addManifest(tag, data, mediaType)
	switch {
	case r.resolver.outDir != "":
		// The OCI image layout does not distinguish between data blobs and
		// manifest blobs, unlike the OCI registry API, so use PushBlob
		// to create the blob.
		return r.PushBlob(ctx, repoName, desc, bytes.NewReader(data))
	case r.resolver.dryRun:
		return desc, nil
	default:
		return r.registry.PushManifest(ctx, repoName, tag, data, mediaType)
	}
}
