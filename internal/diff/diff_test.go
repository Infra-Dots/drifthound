package diff

import (
	"testing"

	"drifthound/internal/resource"
	"drifthound/internal/state"
)

func TestComputeBuckets(t *testing.T) {
	codeOnly := resource.Resource{ARN: "arn:aws:iam::111:role/Code", Account: "111", Origin: resource.OriginCode}
	tracked := resource.Resource{ARN: "arn:aws:s3:::tracked-bucket", Account: "111", Origin: resource.OriginCode}
	driftRes := resource.Resource{ARN: "arn:aws:ec2:us-east-1:111:instance/i-drift", Account: "111", Region: "us-east-1", Origin: resource.OriginCloud}
	unscopedRes := resource.Resource{ARN: "arn:aws:ec2:ap-south-1:111:instance/i-unscoped", Account: "111", Region: "ap-south-1", Origin: resource.OriginCloud}
	cloudTracked := resource.Resource{ARN: tracked.ARN, Account: "111", Origin: resource.OriginCloud}

	states := []ScopedState{{
		Name:      "prod",
		Scope:     state.Scope{Accounts: []string{"111"}, Regions: []string{"us-east-1"}},
		Resources: []resource.Resource{tracked, codeOnly},
	}}

	res := Compute([]resource.Resource{cloudTracked, driftRes, unscopedRes}, states)

	if len(res.Drift) != 1 || res.Drift[0].ARN != driftRes.ARN {
		t.Errorf("drift = %+v, want [%s]", res.Drift, driftRes.ARN)
	}
	if len(res.Unscoped) != 1 || res.Unscoped[0].ARN != unscopedRes.ARN {
		t.Errorf("unscoped = %+v, want [%s]", res.Unscoped, unscopedRes.ARN)
	}
	if len(res.Ghost) != 1 || res.Ghost[0].ARN != codeOnly.ARN {
		t.Errorf("ghost = %+v, want [%s]", res.Ghost, codeOnly.ARN)
	}
}
