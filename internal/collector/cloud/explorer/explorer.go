package explorer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2/types"

	"drifthound/internal/collector/cloud"
	"drifthound/internal/resource"
)

// Collector enumerates live AWS resources via Resource Explorer.
//
// Resource Explorer must be enabled in the account with a multi-region
// aggregator index, or the caller must supply a view ARN explicitly. The
// aggregator's region is auto-detected via ListIndexes so the user doesn't
// have to know where it lives.
type Collector struct {
	// ViewARN, if set, is used directly and the aggregator-discovery step is
	// skipped. The client region is taken from the ARN.
	ViewARN string

	// Regions, if non-empty, becomes a `region:<r> OR ...` filter on Search.
	Regions []string

	// ExcludeServices, if non-empty, adds `-service:<name>` clauses to the
	// Search query so the API never returns those resources.
	ExcludeServices []string
}

func New() *Collector { return &Collector{} }

func (c *Collector) Name() string       { return "resource-explorer" }
func (c *Collector) Scope() cloud.Scope { return cloud.ScopeRegional }

func (c *Collector) Enumerate(ctx context.Context, cfg aws.Config) (<-chan resource.Resource, <-chan error) {
	out := make(chan resource.Resource)
	errs := make(chan error, 1)
	go c.run(ctx, cfg, out, errs)
	return out, errs
}

func (c *Collector) run(ctx context.Context, cfg aws.Config, out chan<- resource.Resource, errs chan<- error) {
	defer close(out)
	defer close(errs)

	client, viewARN, err := c.resolve(ctx, cfg)
	if err != nil {
		errs <- err
		return
	}

	query := buildQuery(c.Regions, c.ExcludeServices)
	var token *string
	for {
		resp, err := client.Search(ctx, &resourceexplorer2.SearchInput{
			QueryString: &query,
			ViewArn:     &viewARN,
			NextToken:   token,
		})
		if err != nil {
			errs <- fmt.Errorf("resource explorer search: %w", err)
			return
		}
		for _, r := range resp.Resources {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case out <- toResource(r):
			}
		}
		if resp.NextToken == nil {
			return
		}
		token = resp.NextToken
	}
}

func (c *Collector) resolve(ctx context.Context, cfg aws.Config) (*resourceexplorer2.Client, string, error) {
	if c.ViewARN != "" {
		region, err := regionFromViewARN(c.ViewARN)
		if err != nil {
			return nil, "", err
		}
		rc := cfg.Copy()
		rc.Region = region
		return resourceexplorer2.NewFromConfig(rc), c.ViewARN, nil
	}

	aggRegion, err := findAggregatorRegion(ctx, cfg)
	if err != nil {
		return nil, "", err
	}
	rc := cfg.Copy()
	rc.Region = aggRegion
	client := resourceexplorer2.NewFromConfig(rc)

	viewARN, err := resolveDefaultView(ctx, client)
	if err != nil {
		return nil, "", err
	}
	return client, viewARN, nil
}

func findAggregatorRegion(ctx context.Context, cfg aws.Config) (string, error) {
	client := resourceexplorer2.NewFromConfig(cfg)
	resp, err := client.ListIndexes(ctx, &resourceexplorer2.ListIndexesInput{
		Type: types.IndexTypeAggregator,
	})
	if err != nil {
		return "", fmt.Errorf("list resource explorer indexes: %w", err)
	}
	if len(resp.Indexes) == 0 {
		return "", errors.New("no Resource Explorer aggregator index found — enable Resource Explorer with multi-region aggregation, or pass --explorer-view <arn>")
	}
	if resp.Indexes[0].Region == nil {
		return "", errors.New("aggregator index has no region")
	}
	return *resp.Indexes[0].Region, nil
}

func resolveDefaultView(ctx context.Context, client *resourceexplorer2.Client) (string, error) {
	resp, err := client.GetDefaultView(ctx, &resourceexplorer2.GetDefaultViewInput{})
	if err != nil {
		return "", fmt.Errorf("get default resource explorer view: %w", err)
	}
	if resp.ViewArn == nil || *resp.ViewArn == "" {
		return "", errors.New("no default Resource Explorer view set in aggregator region — set one in the console or pass --explorer-view <arn>")
	}
	return *resp.ViewArn, nil
}

func regionFromViewARN(arn string) (string, error) {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 || parts[0] != "arn" {
		return "", fmt.Errorf("invalid view ARN: %s", arn)
	}
	if parts[3] == "" {
		return "", fmt.Errorf("view ARN %s has no region", arn)
	}
	return parts[3], nil
}

func buildQuery(regions, excludeServices []string) string {
	var parts []string
	if len(regions) > 0 {
		regionParts := make([]string, 0, len(regions))
		for _, r := range regions {
			regionParts = append(regionParts, "region:"+r)
		}
		if len(regionParts) > 1 {
			parts = append(parts, "("+strings.Join(regionParts, " OR ")+")")
		} else {
			parts = append(parts, regionParts[0])
		}
	}
	for _, svc := range excludeServices {
		parts = append(parts, "-service:"+svc)
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, " ")
}

func toResource(r types.Resource) resource.Resource {
	res := resource.Resource{Origin: resource.OriginCloud}
	if r.Arn != nil {
		res.ARN = *r.Arn
		svc, region, account := resource.ParseARNScope(res.ARN)
		res.Service = svc
		res.Region = region
		res.Account = account
	}
	// Resource Explorer's own fields are more reliable than ARN parsing for
	// services that put weird stuff in the region/account ARN slots — let
	// them win.
	if r.OwningAccountId != nil {
		res.Account = *r.OwningAccountId
	}
	if r.Region != nil {
		res.Region = *r.Region
	}
	if r.ResourceType != nil {
		res.Type = *r.ResourceType
	}
	if r.Service != nil {
		res.Service = *r.Service
	}
	if tags := extractTags(r.Properties); tags != nil {
		res.Tags = tags
	}
	return res
}

func extractTags(props []types.ResourceProperty) map[string]string {
	for _, p := range props {
		if p.Name == nil || *p.Name != "tags" || p.Data == nil {
			continue
		}
		var pairs []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		}
		if err := p.Data.UnmarshalSmithyDocument(&pairs); err != nil {
			continue
		}
		out := make(map[string]string, len(pairs))
		for _, kv := range pairs {
			out[kv.Key] = kv.Value
		}
		return out
	}
	return nil
}
