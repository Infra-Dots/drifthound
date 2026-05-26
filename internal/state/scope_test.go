package state

import "testing"

func TestScopeCovers(t *testing.T) {
	cases := []struct {
		name    string
		scope   Scope
		account string
		region  string
		want    bool
	}{
		{"wildcard covers everything", Scope{}, "111", "us-east-1", true},
		{"account match, region match", Scope{Accounts: []string{"111"}, Regions: []string{"us-east-1"}}, "111", "us-east-1", true},
		{"account mismatch", Scope{Accounts: []string{"111"}}, "222", "us-east-1", false},
		{"region mismatch", Scope{Accounts: []string{"111"}, Regions: []string{"us-east-1"}}, "111", "eu-west-1", false},
		{"global resource passes when account matches", Scope{Accounts: []string{"111"}, Regions: []string{"us-east-1"}}, "111", "", true},
		{"wildcard region accepts any", Scope{Accounts: []string{"111"}}, "111", "ap-south-1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.scope.Covers(tc.account, tc.region); got != tc.want {
				t.Errorf("Covers(%q,%q) = %v, want %v", tc.account, tc.region, got, tc.want)
			}
		})
	}
}
