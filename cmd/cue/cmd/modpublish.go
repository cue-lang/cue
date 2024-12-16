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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociref"
	"github.com/opencontainers/go-digest"
	ocispecroot "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/internal/mod/semver"
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
		Long: `Publish the current module to an OCI registry. It consults
$CUE_REGISTRY to determine where the module should be published (see
"cue help environment" for details). Also note that this command does
no dependency or other checks at the moment.

When the --dry-run flag is specified, nothing will actually be written
to a registry, but all other checks will take place.

The --json flag can be used to find out more information about the upload.

The --out flag can be used to write the module's contents to a directory
in OCI Image Layout format. See this link for more details on the format:
https://github.com/opencontainers/image-spec/blob/8f3820ccf8f65db8744e626df17fe8a64462aece/image-layout.md

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, runModUpload),
		Args: cobra.ExactArgs(1),
	}
	cmd.Flags().BoolP(string(flagDryRun), "n", false, "only run simulation")
	cmd.Flags().Bool(string(flagJSON), false, "print verbose information in JSON format (implies --dry-run)")
	cmd.Flags().String(string(flagOut), "", "write module contents to specified directory in OCI Image Layout format (implies --dry-run)")

	return cmd
}

// publishInfo defines the format of the JSON printed by `cue mod publish --json`.
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
	dryRun := flagDryRun.Bool(cmd)
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

	// Ensure that the module is tidy before publishing it.
	// TODO: can we use modload.CheckTidy on the already-parsed modfile.File below?
	// TODO: we might want to provide a "force" flag to skip this check,
	// particularly for cases where one has private deps or pushes to a custom registry.
	reg, err := getCachedRegistry()
	if err != nil {
		return err
	}
	if err := modload.CheckTidy(ctx, os.DirFS(modRoot), ".", reg); err != nil {
		return suggestModCommand(err)
	}

	modPath := filepath.Join(modRoot, "cue.mod/module.cue")
	modfileData, err := os.ReadFile(modPath)
	if err != nil {
		return err
	}
	mf, err := modfile.Parse(modfileData, modPath)
	if err != nil {
		return err
	}
	if !semver.IsValid(args[0]) {
		return fmt.Errorf("invalid publish version %q; must be valid semantic version (see http://semver.org)", args[0])
	}
	if semver.Canonical(args[0]) != args[0] {
		return fmt.Errorf("publish version %q is not in canonical form", args[0])
	}

	if major := mf.MajorVersion(); semver.Major(args[0]) != major {
		if _, _, ok := module.SplitPathVersion(mf.Module); ok {
			return fmt.Errorf("publish version %q does not match the major version %q declared in %q; must be %s.N.N", args[0], major, modPath, major)
		} else {
			return fmt.Errorf("publish version %q does not match implied major version %q in %q; must be %s.N.N", args[0], major, modPath, major)
		}
	}

	mv, err := module.NewVersion(mf.QualifiedModule(), args[0])
	if err != nil {
		return fmt.Errorf("cannot form module version: %v", err)
	}
	if mf.Source == nil {
		// The source field started being a requirement to publish modules in v0.9.0-alpha.2.
		// For backwards compatibility with existing modules which were already being published,
		// we assume that any earlier version means "self", mimicking the old behavior.
		// TODO: perhaps some of this logic, or at least the alpha.2 version, should be in modfile.
		if semver.Compare(mf.Language.Version, "v0.9.0-alpha.2") < 0 {
			mf.Source = &modfile.Source{Kind: "self"}
		} else {
			// TODO print filename relative to current directory
			return fmt.Errorf("no source field found in cue.mod/module.cue")
		}
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
		status, err := vcsImpl.Status(ctx, modRoot)
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

		archive := make([]pathAbsPair, len(files))
		for i, f := range files {
			archive[i] = pathAbsPair{
				path: f,
				abs:  filepath.Join(modRoot, f),
			}
		}

		// Do we have a LICENSE at the root of the module? If not try to grab one
		// from the root of the VCS repo, first ensuring that git is clean with
		// respect to that file too.
		//
		// TODO: work out whether we can/should consider alternative spellings of
		// "LICENSE" here. For example with .txt extensions. For context
		// https://go.dev/ref/mod#vcs-license says:
		//
		// This special case allows the same LICENSE file to apply to all modules
		// within a repository. This only applies to files named LICENSE
		// specifically, without extensions like .txt. Unfortunately, this cannot
		// be extended without breaking cryptographic sums of existing modules;
		// see Authenticating modules. Other tools and websites like pkg.go.dev
		// may recognize files with other names.
		//
		// https://pkg.go.dev/golang.org/x/pkgsite/internal/licenses#pkg-variables
		// has a much longer list of files.
		haveLICENSE := slices.Contains(files, "LICENSE")
		if !haveLICENSE && modRoot != vcsImpl.Root() {
			licenseFile, err := rootLICENSEFile(ctx, vcsImpl)
			if err != nil {
				return err
			}
			if licenseFile != "" {
				archive = append(archive, pathAbsPair{
					path: "LICENSE",
					abs:  licenseFile,
				})
			}
		}

		if err := modzip.Create(zf, mv, archive, pathAbsPairIO{}); err != nil {
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

	// Do not output full OCI references by default without --json, as sources
	// like git may cause non-deterministic digests due to commit hashes
	// including timestamps. Most users shouldn't care about full digests in
	// most cases, and any non-determinism causes issues for reproducible guides
	// such as those under cuelang.org.
	//
	// TODO: implement -v or similar to include the full ociref.Reference form.
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
		// See comment above about short vs regular OCI reference output.
		fmt.Printf("dry-run published %s to %v\n", mv, shortString(ref))
	default:
		// See comment above about short vs regular OCI reference output.
		fmt.Printf("published %s to %v\n", mv, shortString(ref))
	}
	return nil
}

// rootLICENSEFile returns the absolute path of a LICENSE file if one
// exists under the control of vcsImpl, and that file is "clean" with
// respect to the current commit. If no LICENSE file exists, then an
// empty string is returned.
func rootLICENSEFile(ctx context.Context, vcsImpl vcs.VCS) (string, error) {
	licenseFile := filepath.Join(vcsImpl.Root(), "LICENSE")

	// relLicenseFile is used in a couple of error situations below
	relLicenseFile := func() string {
		rel, err := filepath.Rel(rootWorkingDir, licenseFile)
		if err == nil {
			return rel
		}
		return licenseFile
	}

	licenseFileInfo, err := os.Lstat(licenseFile)
	if err != nil {
		// If the LICENSE file does not exist, this is not an error. We just
		// have nothing to include.
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if !licenseFileInfo.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", relLicenseFile())
	}

	// Verify that the LICENSE file is "clean" with respect to the commit
	status, err := vcsImpl.Status(ctx, licenseFile)
	if err != nil {
		return "", err
	}
	if status.Uncommitted {
		return "", fmt.Errorf("VCS state is not clean for %s", relLicenseFile())
	}

	return licenseFile, nil
}

// shortString returns a shortened form of an OCI reference, basically
// [ociref.Reference.String] minus the trailing digest. The code implementation
// is a copy-paste with exactly that adaptation.
func shortString(ref ociref.Reference) string {
	var buf strings.Builder
	buf.Grow(len(ref.Host) + 1 + len(ref.Repository) + 1 + len(ref.Tag))
	if ref.Host != "" {
		buf.WriteString(ref.Host)
		buf.WriteByte('/')
	}
	buf.WriteString(ref.Repository)
	if len(ref.Tag) > 0 {
		buf.WriteByte(':')
		buf.WriteString(ref.Tag)
	}
	return buf.String()
}

// pathAbsPair is the pair of a path (in a zip archive) and the absolute
// filepath on disk of the file that will be placed at that path.
type pathAbsPair struct {
	path string
	abs  string
}

// pathAbsPairIO implements [modzip.FileIO] for [pathAbsPair] paths.
type pathAbsPairIO struct{}

func (pathAbsPairIO) Path(p pathAbsPair) string {
	return filepath.ToSlash(p.path)
}

func (pathAbsPairIO) Lstat(p pathAbsPair) (fs.FileInfo, error) {
	return os.Lstat(p.abs)
}

func (pathAbsPairIO) Open(p pathAbsPair) (io.ReadCloser, error) {
	return os.Open(p.abs)
}

// publishRegistryResolverShim implements a wrapper around
// modregistry.Resolver that records information about the module being
// published.
//
// If dryRun is true, it does not actually write to the underlying
// registry.
//
// If outDir is non-empty, it also writes the contents of the module to
// that directory.
//
// If recordFiles is true, it records which files are present in the
// module's zip file.
type publishRegistryResolverShim struct {
	resolver    *modconfig.Resolver
	dryRun      bool
	recordFiles bool
	outDir      string

	initDirOnce  sync.Once
	initDirError error

	// mu protects the fields below it.
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
	if err := os.WriteFile(filepath.Join(r.outDir, "index.json"), data, 0o666); err != nil {
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
		r.initDirError = os.WriteFile(filepath.Join(r.outDir, "oci-layout"), []byte(`
{
    "imageLayoutVersion": "1.0.0"
}
`[1:]), 0o666)
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

func filesFromZip(content0 io.Reader, size int64) ([]string, error) {
	// The modregistry code (our only caller) always invokes PushBlob
	// with a ReaderAt, so we can avoid copying all the data by type-asserting
	// to that.
	content, ok := content0.(io.ReaderAt)
	if !ok {
		return nil, fmt.Errorf("internal error: PushBlob invoked without ReaderAt")
	}
	z, err := zip.NewReader(content, size)
	if err != nil {
		return nil, err
	}
	files := make([]string, len(z.File))
	for i, f := range z.File {
		files[i] = f.Name
	}
	return files, nil
}

func (r *publishRegistryShim) PushBlob(ctx context.Context, repoName string, desc ociregistry.Descriptor, content io.Reader) (ociregistry.Descriptor, error) {
	if err := r.resolver.setRepository(repoName); err != nil {
		return ociregistry.Descriptor{}, err
	}
	if r.resolver.recordFiles && desc.MediaType == "application/zip" {
		files, err := filesFromZip(content, desc.Size)
		if err != nil {
			return ociregistry.Descriptor{}, err
		}
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
		if err := f.Close(); err != nil {
			return ociregistry.Descriptor{}, err
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
