package explorer

import "testing"

func TestBuildQuery(t *testing.T) {
	cases := []struct {
		name     string
		regions  []string
		excludes []string
		want     string
	}{
		{"empty returns wildcard", nil, nil, "*"},
		{"single region", []string{"us-east-1"}, nil, "region:us-east-1"},
		{"multiple regions are parenthesised", []string{"us-east-1", "eu-west-1"}, nil, "(region:us-east-1 OR region:eu-west-1)"},
		{"exclude only", nil, []string{"iam"}, "-service:iam"},
		{"region + exclude", []string{"us-east-1"}, []string{"iam"}, "region:us-east-1 -service:iam"},
		{"multi region + multi exclude", []string{"us-east-1", "eu-west-1"}, []string{"iam", "eks"}, "(region:us-east-1 OR region:eu-west-1) -service:iam -service:eks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildQuery(tc.regions, tc.excludes); got != tc.want {
				t.Errorf("buildQuery = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRegionFromViewARN(t *testing.T) {
	cases := []struct {
		name    string
		arn     string
		want    string
		wantErr bool
	}{
		{"valid view arn", "arn:aws:resource-explorer-2:us-east-1:111122223333:view/all-resources/abc-123", "us-east-1", false},
		{"missing region slot", "arn:aws:resource-explorer-2::111122223333:view/all/abc", "", true},
		{"not an arn", "not-an-arn", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := regionFromViewARN(tc.arn)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("region = %q, want %q", got, tc.want)
			}
		})
	}
}
