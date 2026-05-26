package cloud

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"drifthound/internal/resource"
)

type Scope int

const (
	ScopeRegional Scope = iota
	ScopeGlobal
)

// Collector enumerates live AWS resources for a single service. Implementations
// stream results so the differ can start processing before enumeration finishes.
type Collector interface {
	Name() string
	Scope() Scope
	Enumerate(ctx context.Context, cfg aws.Config) (<-chan resource.Resource, <-chan error)
}

// Registry is populated via init() in each service collector package.
type Registry struct {
	collectors []Collector
}

func (r *Registry) Register(c Collector) { r.collectors = append(r.collectors, c) }
func (r *Registry) All() []Collector     { return r.collectors }

var Default = &Registry{}
