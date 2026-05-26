package state

import (
	"context"
	"io"
)

// Source discovers state files from some backing store (local glob, S3, TFC).
type Source interface {
	Discover(ctx context.Context) ([]Ref, error)
}

// Ref is a single discovered state. Scope may be empty when the source can't
// infer it; the caller is expected to backfill from the state's own contents
// or from CLI flags before diffing.
type Ref struct {
	Name   string
	Scope  Scope
	Loader Loader
}

// Loader returns the raw JSON bytes of a state file. Implementations may
// stream from disk, S3, or an API; callers must Close the reader.
type Loader interface {
	Load(ctx context.Context) (io.ReadCloser, error)
}
