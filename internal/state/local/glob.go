package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"drifthound/internal/state"
)

// Source resolves one or more file glob patterns into state.Refs. Patterns are
// evaluated relative to the process working directory.
type Source struct {
	Patterns []string
}

func (s *Source) Discover(_ context.Context) ([]state.Ref, error) {
	var refs []state.Ref
	for _, p := range s.Patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", p, err)
		}
		for _, m := range matches {
			refs = append(refs, state.Ref{
				Name:   m,
				Loader: &fileLoader{path: m},
			})
		}
	}
	return refs, nil
}

type fileLoader struct{ path string }

func (l *fileLoader) Load(_ context.Context) (io.ReadCloser, error) {
	return os.Open(l.path)
}
