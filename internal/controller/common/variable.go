package common

import (
	"runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MaxConcurrentReconciles is the Number of Reconcile threads to run Reconcile operations
var MaxConcurrentReconciles = runtime.NumCPU() * 2

var (
	UpdateOption = &client.UpdateOptions{
		FieldManager: "aerospike-operator",
	}
	CreateOption = &client.CreateOptions{
		FieldManager: "aerospike-operator",
	}
)
