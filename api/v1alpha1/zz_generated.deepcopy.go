// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeAccessControlSpec) DeepCopyInto(out *AerospikeAccessControlSpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeCertPathInOperatorSource) DeepCopyInto(out *AerospikeCertPathInOperatorSource) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeCertPathInOperatorSource.
func (in *AerospikeCertPathInOperatorSource) DeepCopy() *AerospikeCertPathInOperatorSource {
	if in == nil {
		return nil
	}
	out := new(AerospikeCertPathInOperatorSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeClientAdminPolicy) DeepCopyInto(out *AerospikeClientAdminPolicy) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeCluster) DeepCopyInto(out *AerospikeCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeCluster.
func (in *AerospikeCluster) DeepCopy() *AerospikeCluster {
	if in == nil {
		return nil
	}
	out := new(AerospikeCluster)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *AerospikeCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeClusterList) DeepCopyInto(out *AerospikeClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]AerospikeCluster, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeClusterList.
func (in *AerospikeClusterList) DeepCopy() *AerospikeClusterList {
	if in == nil {
		return nil
	}
	out := new(AerospikeClusterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *AerospikeClusterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeClusterSpec) DeepCopyInto(out *AerospikeClusterSpec) {
	*out = *in
	in.Storage.DeepCopyInto(&out.Storage)
	in.AerospikeConfigSecret.DeepCopyInto(&out.AerospikeConfigSecret)
	if in.AerospikeAccessControl != nil {
		in, out := &in.AerospikeAccessControl, &out.AerospikeAccessControl
		*out = (*in).DeepCopy()
	}
	if in.AerospikeConfig != nil {
		in, out := &in.AerospikeConfig, &out.AerospikeConfig
		*out = (*in).DeepCopy()
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(v1.ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.ValidationPolicy != nil {
		in, out := &in.ValidationPolicy, &out.ValidationPolicy
		*out = (*in).DeepCopy()
	}
	in.RackConfig.DeepCopyInto(&out.RackConfig)
	in.AerospikeNetworkPolicy.DeepCopyInto(&out.AerospikeNetworkPolicy)
	if in.OperatorClientCertSpec != nil {
		in, out := &in.OperatorClientCertSpec, &out.OperatorClientCertSpec
		*out = new(AerospikeOperatorClientCertSpec)
		(*in).DeepCopyInto(*out)
	}
	in.PodSpec.DeepCopyInto(&out.PodSpec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeClusterSpec.
func (in *AerospikeClusterSpec) DeepCopy() *AerospikeClusterSpec {
	if in == nil {
		return nil
	}
	out := new(AerospikeClusterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeClusterStatus) DeepCopyInto(out *AerospikeClusterStatus) {
	*out = *in
	in.AerospikeClusterStatusSpec.DeepCopyInto(&out.AerospikeClusterStatusSpec)
	if in.Pods != nil {
		in, out := &in.Pods, &out.Pods
		*out = make(map[string]AerospikePodStatus, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeClusterStatus.
func (in *AerospikeClusterStatus) DeepCopy() *AerospikeClusterStatus {
	if in == nil {
		return nil
	}
	out := new(AerospikeClusterStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeClusterStatusSpec) DeepCopyInto(out *AerospikeClusterStatusSpec) {
	*out = *in
	in.Storage.DeepCopyInto(&out.Storage)
	in.AerospikeConfigSecret.DeepCopyInto(&out.AerospikeConfigSecret)
	if in.AerospikeAccessControl != nil {
		in, out := &in.AerospikeAccessControl, &out.AerospikeAccessControl
		*out = (*in).DeepCopy()
	}
	if in.AerospikeConfig != nil {
		in, out := &in.AerospikeConfig, &out.AerospikeConfig
		*out = (*in).DeepCopy()
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(v1.ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.ValidationPolicy != nil {
		in, out := &in.ValidationPolicy, &out.ValidationPolicy
		*out = (*in).DeepCopy()
	}
	in.RackConfig.DeepCopyInto(&out.RackConfig)
	in.AerospikeNetworkPolicy.DeepCopyInto(&out.AerospikeNetworkPolicy)
	if in.OperatorClientCertSpec != nil {
		in, out := &in.OperatorClientCertSpec, &out.OperatorClientCertSpec
		*out = new(AerospikeOperatorClientCertSpec)
		(*in).DeepCopyInto(*out)
	}
	in.PodSpec.DeepCopyInto(&out.PodSpec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeClusterStatusSpec.
func (in *AerospikeClusterStatusSpec) DeepCopy() *AerospikeClusterStatusSpec {
	if in == nil {
		return nil
	}
	out := new(AerospikeClusterStatusSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeConfigSecretSpec) DeepCopyInto(out *AerospikeConfigSecretSpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeConfigSpec) DeepCopyInto(out *AerospikeConfigSpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeInstanceSummary) DeepCopyInto(out *AerospikeInstanceSummary) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeNetworkPolicy) DeepCopyInto(out *AerospikeNetworkPolicy) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeOperatorCertSource) DeepCopyInto(out *AerospikeOperatorCertSource) {
	*out = *in
	if in.SecretCertSource != nil {
		in, out := &in.SecretCertSource, &out.SecretCertSource
		*out = new(AerospikeSecretCertSource)
		**out = **in
	}
	if in.CertPathInOperator != nil {
		in, out := &in.CertPathInOperator, &out.CertPathInOperator
		*out = new(AerospikeCertPathInOperatorSource)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeOperatorCertSource.
func (in *AerospikeOperatorCertSource) DeepCopy() *AerospikeOperatorCertSource {
	if in == nil {
		return nil
	}
	out := new(AerospikeOperatorCertSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeOperatorClientCertSpec) DeepCopyInto(out *AerospikeOperatorClientCertSpec) {
	*out = *in
	in.AerospikeOperatorCertSource.DeepCopyInto(&out.AerospikeOperatorCertSource)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeOperatorClientCertSpec.
func (in *AerospikeOperatorClientCertSpec) DeepCopy() *AerospikeOperatorClientCertSpec {
	if in == nil {
		return nil
	}
	out := new(AerospikeOperatorClientCertSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikePersistentVolumePolicySpec) DeepCopyInto(out *AerospikePersistentVolumePolicySpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikePersistentVolumeSpec) DeepCopyInto(out *AerospikePersistentVolumeSpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikePodSpec) DeepCopyInto(out *AerospikePodSpec) {
	*out = *in
	if in.Sidecars != nil {
		in, out := &in.Sidecars, &out.Sidecars
		*out = make([]v1.Container, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.InputDNSPolicy != nil {
		in, out := &in.InputDNSPolicy, &out.InputDNSPolicy
		*out = new(v1.DNSPolicy)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikePodSpec.
func (in *AerospikePodSpec) DeepCopy() *AerospikePodSpec {
	if in == nil {
		return nil
	}
	out := new(AerospikePodSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikePodStatus) DeepCopyInto(out *AerospikePodStatus) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeRoleSpec) DeepCopyInto(out *AerospikeRoleSpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeSecretCertSource) DeepCopyInto(out *AerospikeSecretCertSource) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeSecretCertSource.
func (in *AerospikeSecretCertSource) DeepCopy() *AerospikeSecretCertSource {
	if in == nil {
		return nil
	}
	out := new(AerospikeSecretCertSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeStorageSpec) DeepCopyInto(out *AerospikeStorageSpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeUserSpec) DeepCopyInto(out *AerospikeUserSpec) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Rack) DeepCopyInto(out *Rack) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RackConfig) DeepCopyInto(out *RackConfig) {
	clone := in.DeepCopy()
	*out = *clone
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ValidationPolicySpec) DeepCopyInto(out *ValidationPolicySpec) {
	clone := in.DeepCopy()
	*out = *clone
}
