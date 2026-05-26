package state

// Scope describes the accounts and regions a state file is authoritative for.
// Empty slices mean "wildcard" — matches anything.
type Scope struct {
	Accounts []string
	Regions  []string
}

// Covers reports whether this scope claims responsibility for a resource at
// (account, region). A resource with region == "" is treated as global and
// matches any scope that covers the account.
func (s Scope) Covers(account, region string) bool {
	if !matchOrAny(s.Accounts, account) {
		return false
	}
	if region == "" {
		return true
	}
	return matchOrAny(s.Regions, region)
}

func matchOrAny(list []string, v string) bool {
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
