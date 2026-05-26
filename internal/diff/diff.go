package diff

import (
	"drifthound/internal/resource"
	"drifthound/internal/state"
)

// Result buckets cloud/code resources into three categories:
//   - Drift:    cloud-only, but covered by some state's scope (real drift).
//   - Unscoped: cloud-only, and no state claims this account/region.
//   - Ghost:    code-only — listed in state but not found in the cloud scan.
type Result struct {
	Drift    []resource.Resource `json:"drift"`
	Unscoped []resource.Resource `json:"unscoped"`
	Ghost    []resource.Resource `json:"ghost"`
}

// ScopedState attaches the authoritative Scope to a parsed state file's
// resources.
type ScopedState struct {
	Name      string
	Scope     state.Scope
	Resources []resource.Resource
}

// Compute partitions cloud and code resources into Drift / Unscoped / Ghost.
// Cloud resources matched by some state's scope but absent from code are
// drift; cloud resources outside every state's scope are unscoped; code
// resources missing from cloud are ghosts.
func Compute(cloud []resource.Resource, states []ScopedState) Result {
	code := make(map[string]resource.Resource)
	for _, st := range states {
		for _, r := range st.Resources {
			code[r.Key()] = r
		}
	}

	cloudSeen := make(map[string]struct{}, len(cloud))
	var res Result
	for _, r := range cloud {
		cloudSeen[r.Key()] = struct{}{}
		if _, tracked := code[r.Key()]; tracked {
			continue
		}
		if anyCovers(states, r.Account, r.Region) {
			res.Drift = append(res.Drift, r)
		} else {
			res.Unscoped = append(res.Unscoped, r)
		}
	}
	for k, r := range code {
		if _, ok := cloudSeen[k]; !ok {
			res.Ghost = append(res.Ghost, r)
		}
	}
	return res
}

func anyCovers(states []ScopedState, account, region string) bool {
	for _, s := range states {
		if s.Scope.Covers(account, region) {
			return true
		}
	}
	return false
}
