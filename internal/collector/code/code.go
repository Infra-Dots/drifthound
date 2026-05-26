package code

import (
	"context"
	"fmt"

	"drifthound/internal/resource"
	"drifthound/internal/state"
	"drifthound/internal/state/tfstate"
)

// Collect runs every Source, parses each discovered state, and returns the
// resources grouped by ref so the caller can attach scopes per-state.
type Grouped struct {
	Ref       state.Ref
	Resources []resource.Resource
}

func Collect(ctx context.Context, sources []state.Source) ([]Grouped, error) {
	var out []Grouped
	for _, src := range sources {
		refs, err := src.Discover(ctx)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			rc, err := ref.Loader.Load(ctx)
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", ref.Name, err)
			}
			rs, err := tfstate.Parse(rc, ref.Name)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", ref.Name, err)
			}
			out = append(out, Grouped{Ref: ref, Resources: rs})
		}
	}
	return out, nil
}
