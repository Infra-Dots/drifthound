package tfstate

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"drifthound/internal/resource"
)

type fileFormat struct {
	Version          int             `json:"version"`
	TerraformVersion string          `json:"terraform_version"`
	Resources        []resourceEntry `json:"resources"`
}

type resourceEntry struct {
	Mode      string         `json:"mode"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Provider  string         `json:"provider"`
	Instances []resourceInst `json:"instances"`
}

type resourceInst struct {
	Attributes map[string]any `json:"attributes"`
}

// Parse reads a Terraform state JSON document and returns the AWS-managed
// resources within it. Non-AWS providers and data sources are skipped.
func Parse(r io.Reader, sourceName string) ([]resource.Resource, error) {
	var f fileFormat
	if err := json.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}

	var out []resource.Resource
	for _, e := range f.Resources {
		if e.Mode != "managed" || !isAWS(e.Provider) {
			continue
		}
		for _, inst := range e.Instances {
			out = append(out, toResource(e.Type, inst.Attributes))
		}
	}
	return out, nil
}

func isAWS(provider string) bool {
	return strings.Contains(provider, "hashicorp/aws") || strings.Contains(provider, "\"aws\"")
}

func toResource(tfType string, attrs map[string]any) resource.Resource {
	r := resource.Resource{
		Type:   tfType,
		Origin: resource.OriginCode,
	}
	if v, ok := attrs["arn"].(string); ok {
		r.ARN = v
	}
	if v, ok := attrs["id"].(string); ok {
		r.ID = v
	}
	if tags, ok := attrs["tags"].(map[string]any); ok {
		r.Tags = make(map[string]string, len(tags))
		for k, v := range tags {
			if s, ok := v.(string); ok {
				r.Tags[k] = s
			}
		}
	}
	if r.ARN != "" {
		r.Service, r.Region, r.Account = resource.ParseARNScope(r.ARN)
	}
	return r
}
