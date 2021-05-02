package v1alpha1

import (
	"encoding/json"

	lib "github.com/aerospike/aerospike-management-lib"
)

// AerospikeConfig container for unstructured Aerospike server config.
type AerospikeConfigSpec struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	Value map[string]interface{} `json:"-"`
}

// MarshalJSON ensures that the unstructured object produces proper
// JSON when passed to Go's standard JSON library.
func (u *AerospikeConfigSpec) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.Value)
}

// UnmarshalJSON ensures that the unstructured object properly decodes
// JSON when passed to Go's standard JSON library.
func (u *AerospikeConfigSpec) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &u.Value)
}

func (v *AerospikeConfigSpec) DeepCopy() *AerospikeConfigSpec {
	src := *v
	dst := AerospikeConfigSpec{
		Value: map[string]interface{}{},
	}
	lib.DeepCopy(dst, src)
	return &dst
}

// func (in *AerospikeConfigSpec) DeepCopy() *AerospikeConfigSpec {
// 	if in == nil {
// 		return nil
// 	}
// 	out := new(AerospikeConfigSpec)

// 	// TODO: Use deepcopy form management lib
// 	*out = *in
// 	return out
// }
