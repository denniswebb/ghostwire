package iptables

// Config represents iptables/ip6tables configuration options used during setup.
type Config struct {
	ChainName    string
	ExcludeCIDRs []string
	IPv6         bool
	DnatMapPath  string
}

// Rule represents a single iptables rule invocation.
type Rule struct {
	Table    string
	Chain    string
	RuleSpec []string
	Comment  string
}
