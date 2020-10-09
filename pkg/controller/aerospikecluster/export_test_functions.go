// +build test

package aerospikecluster

// Export symbols to be used in testing.

// GetEndpointsFromInfo parses endpoints lists from info output.
var GetEndpointsFromInfo = getEndpointsFromInfo

// ParseInfoIntoMap to parse info output into a map.
var ParseInfoIntoMap = parseInfoIntoMap
