// Copyright (C) 2014-2024 Anduin Transactions Inc.
//
// Anduin maintained source code to patch OCI client behavior

package modregistry

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var logging, _ = strconv.ParseBool(os.Getenv("ANDUIN_CUE_DEBUG"))

type anduinPatch struct {
	originalClient *Client
}

// PutModuleWithMetadata
// Override default put module function to make restructure OCI layer. The goal is to make it compatible with both Oras and Cue cli
// The expected layers are as following:
//  1. All file with annotations of oras
//  2. Cue module file (compatible layer with cue)
//  3. ZIP file with only *.cue file included (compatible layer with cue)
//
// Note: *.cue will be duplicated because Oras will not pull cue layers
func (p *anduinPatch) putCheckedModule(ctx context.Context, m *checkedModule, meta *Metadata) error {
	logf("using patched `putCheckedModule`")

	loc, err := p.originalClient.resolve(m.mv)
	if err != nil {
		return err
	}

	// repack zip file with only *.cue files
	repackZip, err := os.CreateTemp("", "cue-repack-publish-")
	if err != nil {
		return nil
	}
	defer os.Remove(repackZip.Name())
	defer repackZip.Close()

	zw := zip.NewWriter(repackZip)

	// oras only layers
	orasLayers := []ocispec.Descriptor{}

	logf("valid files: %v", m.validFiles)
	for _, zf := range m.zipr.File {
		// only handle valid file
		if !slices.Contains(m.validFiles, zf.Name) {
			continue
		}

		err := func() error {
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			// single buffer for multiple copy operation
			// avoid re-reading zip file
			data, err := io.ReadAll(rc)
			if err != nil {
				return err
			}

			// only add cue file to repack zip
			if strings.HasSuffix(zf.Name, ".cue") {
				if err := addFileToRepack(zw, zf.Name, bytes.NewReader(data)); err != nil {
					return err
				}
			}

			annotation := map[string]string{}
			annotation[ocispec.AnnotationTitle] = zf.Name
			dataLayer := ocispec.Descriptor{
				Digest:      digest.FromBytes(data),
				MediaType:   ocispec.MediaTypeImageLayer,
				Size:        int64(len(data)),
				Annotations: annotation,
			}
			if _, err := loc.Registry.PushBlob(ctx, loc.Repository, dataLayer, bytes.NewReader(data)); err != nil {
				return fmt.Errorf("cannot push oras data layer: %v", err)
			}
			orasLayers = append(orasLayers, dataLayer)

			return nil
		}()
		if err != nil {
			return err
		}

	}

	// flush all buffered zip
	if err := zw.Close(); err != nil {
		return fmt.Errorf("cannot flush repack zip file: %v", err)
	}
	zipStat, err := repackZip.Stat()
	if err != nil {
		return fmt.Errorf("cannot calculate repack zip file stat: %v", err)
	}
	totalZrSize := zipStat.Size()
	logf("total repack zip file: %d", totalZrSize)

	// copy of original put checked module
	annotations, err := extractAnnotationMap(meta)
	if err != nil {
		return err
	}

	zipDigest, err := digest.FromReader(io.NewSectionReader(repackZip, 0, totalZrSize))
	if err != nil {
		return fmt.Errorf("cannot read module zip file: %v", err)
	}
	_, err = repackZip.Seek(0, io.SeekStart) // rewind seek
	if err != nil {
		return fmt.Errorf("cannot rewind repack zip file: %v", err)
	}

	configDesc, err := p.originalClient.scratchConfig(ctx, loc, moduleArtifactType)
	if err != nil {
		return fmt.Errorf("cannot make scratch config: %v", err)
	}

	manifest := &ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers: append(orasLayers,
			ocispec.Descriptor{
				Digest:    digest.FromBytes(m.modFileContent),
				MediaType: moduleFileMediaType,
				Size:      int64(len(m.modFileContent)),
			},
			ocispec.Descriptor{
				Digest:    zipDigest,
				MediaType: "application/zip",
				Size:      totalZrSize,
			},
		),
		Annotations: annotations,
	}

	if _, err := loc.Registry.PushBlob(ctx, loc.Repository, moduleFileLayer(manifest), bytes.NewReader(m.modFileContent)); err != nil {
		return fmt.Errorf("cannot push cue.mod/module.cue contents: %v", err)
	}
	if _, err := loc.Registry.PushBlob(ctx, loc.Repository, zipContentLayer(manifest), io.NewSectionReader(repackZip, 0, totalZrSize)); err != nil {
		return fmt.Errorf("cannot push module contents: %v", err)
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

func addFileToRepack(zr *zip.Writer, name string, r io.Reader) error {
	w, err := zr.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

func extractAnnotationMap(meta *Metadata) (map[string]string, error) {
	if meta == nil {
		return nil, nil
	}
	return meta.annotations()
}

func zipContentLayer(m *ocispec.Manifest) ocispec.Descriptor {
	layerLen := len(m.Layers)
	return m.Layers[layerLen-1]
}

func moduleFileLayer(m *ocispec.Manifest) ocispec.Descriptor {
	layerLen := len(m.Layers)
	return m.Layers[layerLen-2]
}

func logf(f string, a ...any) {
	if logging {
		log.Printf("anduin: "+f, a...)
	}
}
