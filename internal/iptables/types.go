package iptables

// Config represents iptables/ip6tables configuration options used during setup.
type Config struct {
	ChainName    string
	ExcludeCIDRs []string
	IPv6         bool
	DnatMapPath  string
}
