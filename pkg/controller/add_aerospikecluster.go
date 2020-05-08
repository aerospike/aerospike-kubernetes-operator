package controller

import (
	"github.com/citrusleaf/aerospike-kubernetes-operator/pkg/controller/aerospikecluster"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, aerospikecluster.Add)
}
