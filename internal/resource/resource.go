package resource

import "strings"

type Origin string

const (
	OriginCode  Origin = "code"
	OriginCloud Origin = "cloud"
)

type Resource struct {
	ARN     string
	ID      string
	Type    string
	Service string
	Region  string
	Account string
	Tags    map[string]string
	Origin  Origin
}

// Key is the canonical identity used for diffing. ARN when available,
// otherwise a composite that mirrors the same scope (service/region/account).
func (r Resource) Key() string {
	if r.ARN != "" {
		return r.ARN
	}
	return r.Service + ":" + r.Region + ":" + r.Account + ":" + r.Type + ":" + r.ID
}

// ParseARNScope extracts (service, region, account) from a standard AWS ARN.
// Returns zero values for malformed input rather than erroring — callers treat
// missing fields as "unknown".
func ParseARNScope(arn string) (service, region, account string) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return "", "", ""
	}
	return parts[2], parts[3], parts[4]
}
