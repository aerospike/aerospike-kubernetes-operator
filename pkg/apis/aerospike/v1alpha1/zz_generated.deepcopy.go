// +build !ignore_autogenerated

// Code generated by operator-sdk. DO NOT EDIT.

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeAuthSecretSpec) DeepCopyInto(out *AerospikeAuthSecretSpec) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AerospikeAuthSecretSpec.
func (in *AerospikeAuthSecretSpec) DeepCopy() *AerospikeAuthSecretSpec {
	if in == nil {
		return nil
	}
	out := new(AerospikeAuthSecretSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeCluster) DeepCopyInto(out *AerospikeCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
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
	return
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
	if in.BlockStorage != nil {
		in, out := &in.BlockStorage, &out.BlockStorage
		*out = make([]BlockStorageSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.FileStorage != nil {
		in, out := &in.FileStorage, &out.FileStorage
		*out = make([]FileStorageSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.AerospikeConfigSecret.DeepCopyInto(&out.AerospikeConfigSecret)
	out.AerospikeAuthSecret = in.AerospikeAuthSecret
	in.AerospikeConfig.DeepCopyInto(&out.AerospikeConfig)
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(v1.ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.Rack != nil {
		in, out := &in.Rack, &out.Rack
		*out = make([]RackSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
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
	in.AerospikeClusterSpec.DeepCopyInto(&out.AerospikeClusterSpec)
	if in.Nodes != nil {
		in, out := &in.Nodes, &out.Nodes
		*out = make([]AerospikeNodeSummary, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
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
func (in *AerospikeConfigSecretSpec) DeepCopyInto(out *AerospikeConfigSecretSpec) {
	clone := in.DeepCopy()
	*out = *clone
	return
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AerospikeNodeSummary) DeepCopyInto(out *AerospikeNodeSummary) {
	clone := in.DeepCopy()
	*out = *clone
	return
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BlockStorageSpec) DeepCopyInto(out *BlockStorageSpec) {
	clone := in.DeepCopy()
	*out = *clone
	return
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *FileStorageSpec) DeepCopyInto(out *FileStorageSpec) {
	clone := in.DeepCopy()
	*out = *clone
	return
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RackSpec) DeepCopyInto(out *RackSpec) {
	clone := in.DeepCopy()
	*out = *clone
	return
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in Values) DeepCopyInto(out *Values) {
	{
		in := &in
		clone := in.DeepCopy()
		*out = *clone
		return
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VolumeDevice) DeepCopyInto(out *VolumeDevice) {
	clone := in.DeepCopy()
	*out = *clone
	return
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VolumeMount) DeepCopyInto(out *VolumeMount) {
	clone := in.DeepCopy()
	*out = *clone
	return
}
