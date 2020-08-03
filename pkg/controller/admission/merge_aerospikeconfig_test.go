package admission

import (
	"reflect"
	"testing"
)

// base:base, patch:patch
var baseMap = map[string]interface{}{
	"k": map[string]interface{}{
		"k0": "v1",
		"k1": map[string]interface{}{
			"k11": "v11",
		},
		"k2": map[string]interface{}{
			"k21": "v21",
		},
	},
	"service": map[string]interface{}{
		"feature-key-file": "features.conf",
	},
	"namespace": []interface{}{
		map[string]interface{}{
			"name":               "test",
			"replication-factor": 2,
			"storage-engine": map[string]interface{}{
				"file":           []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		map[string]interface{}{
			"name": "bar",
			"storage-engine": map[string]interface{}{
				"file":           []interface{}{"bar.dat"},
				"data-in-memory": true,
			},
		},
	},
}

var patchMap = map[string]interface{}{
	"k": map[string]interface{}{
		"k1": "v2",
		"k2": map[string]interface{}{
			"k21": "v21",
			"k22": "v22",
		},
	},
	"service": map[string]interface{}{
		"proto-fd-max": 15000,
	},
	"namespace": []interface{}{
		map[string]interface{}{
			"name":               "test",
			"replication-factor": 3,
			"storage-engine": map[string]interface{}{
				"device":         []interface{}{"dev"},
				"data-in-memory": true,
			},
		},
		map[string]interface{}{
			"name": "bar",
			"storage-engine": map[string]interface{}{
				"file": []interface{}{"newbar.dat"},
			},
		},
		map[string]interface{}{
			"name":           "asd",
			"storage-engine": "memory",
		},
	},
}

var mergedMap = map[string]interface{}{
	"k": map[string]interface{}{
		"k0": "v1",
		"k1": "v2",
		"k2": map[string]interface{}{
			"k21": "v21",
			"k22": "v22",
		},
	},
	"service": map[string]interface{}{
		"feature-key-file": "features.conf",
		"proto-fd-max":     15000,
	},
	"namespace": []interface{}{
		map[string]interface{}{
			"name":               "test",
			"replication-factor": 3,
			// storage-engine type changed from file to device. So full storage-engine is replaced
			"storage-engine": map[string]interface{}{
				"device":         []interface{}{"dev"},
				"data-in-memory": true,
			},
		},
		map[string]interface{}{
			"name": "bar",
			// storage-engine type not changed, update file only
			"storage-engine": map[string]interface{}{
				"file":           []interface{}{"newbar.dat"},
				"data-in-memory": true,
			},
		},
		// Add new namespace
		map[string]interface{}{
			"name":           "asd",
			"storage-engine": "memory",
		},
	},
}

func TestConfigMerge(t *testing.T) {
	m, err := merge(baseMap, patchMap)
	if err != nil {
		t.Fatalf("failed to merge aerospike config baseMap and patchMap: %v", err)
	}
	if !reflect.DeepEqual(m, mergedMap) {
		t.Fatalf("Merge failed, expected %v, got %v", mergedMap, m)
	}
	t.Logf("AerospikeConfig merged successfully %v", m)
}
