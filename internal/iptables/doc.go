// Package iptables orchestrates the init-container side of ghostwire's routing
// flow. It is responsible for creating and priming the CANARY_DNAT chain,
// adding exclusion RETURN rules, writing DNAT targets for each service mapping,
// and emitting an audit map. The watcher never touches these helpers; it only
// adds or removes the single jump into the configured chain at runtime.
package iptables
