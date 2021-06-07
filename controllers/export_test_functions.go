// +build test

package controllers

// Export symbols to be used in testing.

// TODO: Made all these func public. check func def. Can we make them private and use tags. Need to check how that works

// GetEndpointsFromInfo parses endpoints lists from info output.
var GetEndpointsFromInfo = getEndpointsFromInfo

// ParseInfoIntoMap to parse info output into a map.
var ParseInfoIntoMap = parseInfoIntoMap
