package tfcsource

import (
	"reflect"
	"testing"
)

func TestSelectWorkspaces(t *testing.T) {
	all := []workspace{
		{ID: "1", Name: "prod-network"},
		{ID: "2", Name: "prod-compute"},
		{ID: "3", Name: "staging-network"},
		{ID: "4", Name: "dev-network"},
	}

	cases := []struct {
		name     string
		exact    []string
		prefixes []string
		want     []string
	}{
		{"no filter returns all", nil, nil, []string{"prod-network", "prod-compute", "staging-network", "dev-network"}},
		{"single exact name", []string{"prod-network"}, nil, []string{"prod-network"}},
		{"multiple exact names", []string{"prod-network", "dev-network"}, nil, []string{"prod-network", "dev-network"}},
		{"single prefix", nil, []string{"prod-"}, []string{"prod-network", "prod-compute"}},
		{"multiple prefixes", nil, []string{"prod-", "dev-"}, []string{"prod-network", "prod-compute", "dev-network"}},
		{"exact and prefix union", []string{"staging-network"}, []string{"dev-"}, []string{"staging-network", "dev-network"}},
		{"overlap is not duplicated", []string{"prod-network"}, []string{"prod-"}, []string{"prod-network", "prod-compute"}},
		{"unmatched returns empty", []string{"nonexistent"}, []string{"nope-"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectWorkspaces(all, tc.exact, tc.prefixes)
			names := make([]string, len(got))
			for i, w := range got {
				names[i] = w.Name
			}
			if !reflect.DeepEqual(names, tc.want) && (len(names) != 0 || len(tc.want) != 0) {
				t.Errorf("got %v, want %v", names, tc.want)
			}
		})
	}
}
