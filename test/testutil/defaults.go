// Package testutil provides shared test helpers used across multiple test packages
// (e.g. envtests, cluster). Prefer this package for generic utilities; keep
// cluster-specific helpers in test/cluster.
package testutil

import "fmt"

const (
	// BaseEnterpriseImage is the repo for Aerospike Enterprise server images.
	BaseEnterpriseImage = "aerospike/aerospike-server-enterprise"
	LatestServerVersion = "8.1.1.0"
	StorageClass        = "ssd"
)

var (
	LatestEnterpriseImage = fmt.Sprintf("%s:%s", BaseEnterpriseImage, LatestServerVersion)
)

// DefaultEnterpriseImage returns the full image string for the default (or given)
// Aerospike Enterprise server version. Use this from envtests or any other packages
// when you need a valid image.
func DefaultEnterpriseImage(version string) string {
	if version == "" {
		version = LatestServerVersion
	}

	return BaseEnterpriseImage + ":" + version
}
