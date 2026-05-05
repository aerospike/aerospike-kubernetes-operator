// Package testutil provides shared test helpers used across multiple test packages
// (e.g. envtests, cluster). Prefer this package for generic utilities; keep
// cluster-specific helpers in test/cluster.
package testutil

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	// BaseEnterpriseImage is the repo for Aerospike Enterprise server images.
	BaseEnterpriseImage = "aerospike/aerospike-server-enterprise"
	BaseFederalImage    = "aerospike/aerospike-server-federal"
	LatestServerVersion = "8.1.2.0"
	Pre810ServerVersion = "8.0.0.0"
	// CgroupMemTrackingServerVersion is the minimum Aerospike server version that requires
	// aerospikeConfig.service.cgroup-mem-tracking to be set to true.
	CgroupMemTrackingServerVersion = "8.1.2.0"
	InvalidImageVersion            = "3.0.0.4"
	StorageClass                   = "ssd"
	ClusterNameConfig              = "cluster-name"
	DefaultNamespace               = "default"
	DefaultDevicePath              = "/test/dev/xvdf"
)

var (
	LatestEnterpriseImage = fmt.Sprintf("%s:%s", BaseEnterpriseImage, LatestServerVersion)
	Pre810EnterpriseImage = fmt.Sprintf("%s:%s", BaseEnterpriseImage, Pre810ServerVersion)
	Pre810FederalImage    = fmt.Sprintf("%s:%s", BaseFederalImage, Pre810ServerVersion)
	LatestFederalImage    = fmt.Sprintf("%s:%s", BaseFederalImage, LatestServerVersion)
	InvalidImage          = fmt.Sprintf("%s:%s", BaseEnterpriseImage, InvalidImageVersion)
)

// GetEnterpriseImage returns the full image string for the default (or given)
// Aerospike Enterprise server version. Use this from envtests or any other packages
// when you need a valid image.
func GetEnterpriseImage(version string) string {
	if version == "" {
		version = LatestServerVersion
	}

	return BaseEnterpriseImage + ":" + version
}

func GetGitRepoRootPath() (string, error) {
	path, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(path)), nil
}
