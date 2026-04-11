package main

import "codeberg.org/btsemm/btsemm"

// strategies maps names to constructors. Strategies self-register via init().
var strategies = map[string]func() btsemm.Strategy{}

// registerStrategy adds a strategy to the registry. Call from init() in each strategy file.
func registerStrategy(name string, ctor func() btsemm.Strategy) {
	strategies[name] = ctor
}
