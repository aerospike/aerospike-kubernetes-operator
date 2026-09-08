/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package v1 contains API Schema definitions for the asdb v1 API group
// +kubebuilder:object:generate=true
// +groupName=asdb.aerospike.com
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "asdb.aerospike.com", Version: "v1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &SchemeBuilderType{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// SchemeBuilderType is a minimal, dependency-free replacement for the deprecated
// sigs.k8s.io/controller-runtime/pkg/scheme.Builder, kept local so this api
// package doesn't depend on controller-runtime.
// +kubebuilder:object:generate=false
type SchemeBuilderType struct {
	GroupVersion schema.GroupVersion
	runtime.SchemeBuilder
}

// Register adds one or more objects to the SchemeBuilder so they can be added to a Scheme.
func (bld *SchemeBuilderType) Register(object ...runtime.Object) *SchemeBuilderType {
	bld.SchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(bld.GroupVersion, object...)
		metav1.AddToGroupVersion(scheme, bld.GroupVersion)

		return nil
	})

	return bld
}
