package v1beta1

import (
	"encoding/json"

	lib "github.com/aerospike/aerospike-management-lib"
)

// AerospikeConfigSpec container for unstructured Aerospike server config.
type AerospikeConfigSpec struct {
	Value map[string]interface{} `json:"-"`
}

// MarshalJSON ensures that the unstructured object produces proper
// JSON when passed to Go's standard JSON library.
func (c *AerospikeConfigSpec) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Value)
}

// UnmarshalJSON ensures that the unstructured object properly decodes
// JSON when passed to Go's standard JSON library.
func (c *AerospikeConfigSpec) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &c.Value)
}

func (c *AerospikeConfigSpec) DeepCopy() *AerospikeConfigSpec {
	src := *c
	dst := AerospikeConfigSpec{
		Value: map[string]interface{}{},
	}
	lib.DeepCopy(dst.Value, src.Value)

	return &dst
}
