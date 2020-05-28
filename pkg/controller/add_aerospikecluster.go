package controller

import (
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/aerospikecluster"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, aerospikecluster.Add)
}
