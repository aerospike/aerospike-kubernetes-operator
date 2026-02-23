// Package testutil provides shared test utilities for use across test packages.
//
// Use this package when you have helpers that are:
//   - Generic (e.g. default image strings, retry config, K8s name helpers), and
//   - Used by multiple packages (envtests, cluster, etc.)
//
// Usage from another package:
//
//	import testutil "github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
//	image := testutil.DefaultEnterpriseImage("8.1.1.0")
//	opts := testutil.DefaultRetry()
package testutil
