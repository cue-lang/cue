package cuelang

import (
	"context"
	"path/filepath"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

func Definition(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle, position protocol.Position) ([]protocol.Location, error) {
	// TODO(myitcv): create a context here? Probably need/want the want from the cache?
	cuectx := cuecontext.New()

	byts, err := fh.Content()
	if err != nil {
		return nil, err // TODO(myitcv): what error scenario is this? Add a comment
	}

	// Do a lightweight parse to get the package clause
	//
	// TODO(myitcv): feels like the file.Handle should be able to give us this
	// information. It's going to be pretty ubiquitously used.
	f, err := parser.ParseFile(filepath.Base(fh.URI().Path()), byts, parser.PackageClauseOnly)
	if err != nil {
		// TODO(myitcv): document this error scenario. Because with the parser
		// recovery mode which attempts to "fix" a bad file, should we even be in
		// this method?
		return nil, err
	}

	// TODO(myitcv): need to define (and test) what it means to be working on
	// CUE files that are not part of a package. In the simplest situation we
	// could assume that a file without a package clause is intended to be used
	// as such. But more research and exploration required in this space.
	//
	// For now therefore, Definition will only work on CUE files that are part of
	// a package.

	dir := filepath.Dir(fh.URI().Path())
	conf := &load.Config{
		Dir:     dir,
		Package: f.PackageName(),
	}
	// TODO(myitcv): the build instance for the package lives in the cache...
	bis := load.Instances([]string{"."}, conf)
	bi := bis[0]

	if err := bi.Err; err != nil {
		return nil, err // TODO(myitcv): should thej
	}

	// // TODO(myitcv): ... as do the parsed files belonging to that package
	// var files []*ast.File
	// for _, f := range bi.BuildFiles {
	// 	cf, err := parser.ParseFile(f.Filename, f.Source)
	// 	if err != nil {
	// 		return nil, err // TODO(myitcv): again... semantics required here for whether this should ever happen
	// 	}
	// 	files = append()

	// }

	// TODO(myitcv): ... as does the package value
	v := cuectx.BuildInstance(bis[0])

	if err := v.Err(); err != nil {
		return nil, err // TODO(myitcv): improve error returned and add test
	}

	return nil, nil
}
