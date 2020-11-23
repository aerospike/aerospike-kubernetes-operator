package admission

import (
	"reflect"
	"testing"
)

// basic merge
var baseMap1 = map[string]interface{}{
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
}
var patchMap1 = map[string]interface{}{
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
}
var mergedMap1 = map[string]interface{}{
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
}

// file to dev/mem/patch
var baseMap2 = map[string]interface{}{

	"namespaces": []interface{}{
		// for file type to device type change
		map[string]interface{}{
			"name":               "filetodev",
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// for file type to memory type change
		map[string]interface{}{
			"name":               "filetomem",
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// for file type patch
		map[string]interface{}{
			"name":               "filepatch",
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// extra
		map[string]interface{}{
			"name":               "extrabase",
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
	},
}
var patchMap2 = map[string]interface{}{
	"namespaces": []interface{}{
		// for file type to device type change
		map[string]interface{}{
			"name": "filetodev",
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/test/dev/xvdf"},
				"data-in-memory": true,
			},
		},
		// for file type to memory type change
		map[string]interface{}{
			"name":           "filetomem",
			"storage-engine": "memory",
		},
		// for file type patch
		map[string]interface{}{
			"name": "filepatch",
			"storage-engine": map[string]interface{}{
				"files": []interface{}{"newtest.dat"},
			},
		},
		// extra
		map[string]interface{}{
			"name":               "extrapatch",
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
	},
}
var mergedMap2 = map[string]interface{}{
	"namespaces": []interface{}{
		// for file type to device type change
		map[string]interface{}{
			"name":               "filetodev",
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/test/dev/xvdf"},
				"data-in-memory": true,
			},
		},
		// for file type to memory type change
		map[string]interface{}{
			"name":               "filetomem",
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
		// for file type patch
		map[string]interface{}{
			"name":               "filepatch",
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"newtest.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// extra
		map[string]interface{}{
			"name":               "extrabase",
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
		// extra
		map[string]interface{}{
			"name":               "extrapatch",
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
	},
}

// dev to file/mem/patch
var baseMap3 = map[string]interface{}{
	"namespaces": []interface{}{
		// for device type to file type change
		map[string]interface{}{
			"name":               "devtofile",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/test/dev/xvdf"},
				"data-in-memory": true,
			},
		},
		// for device type to memory type change
		map[string]interface{}{
			"name":               "devtomem",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/test/dev/xvdf"},
				"data-in-memory": true,
			},
		},
		// for device type patch
		map[string]interface{}{
			"name":               "devpatch",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/test/dev/xvdf"},
				"data-in-memory": true,
			},
		},
	},
}
var patchMap3 = map[string]interface{}{
	"namespaces": []interface{}{
		// for device type to file type change
		map[string]interface{}{
			"name": "devtofile",
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// for device type to memory type change
		map[string]interface{}{
			"name":           "devtomem",
			"storage-engine": "memory",
		},
		// for device type patch
		map[string]interface{}{
			"name": "devpatch",
			"storage-engine": map[string]interface{}{
				"devices": []interface{}{"/newtest/dev/xvdf"},
			},
		},
	},
}
var mergedMap3 = map[string]interface{}{
	"namespaces": []interface{}{
		// for device type to file type change
		map[string]interface{}{
			"name":               "devtofile",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// for device type to memory type change
		map[string]interface{}{
			"name":               "devtomem",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
		// for device type patch
		map[string]interface{}{
			"name":               "devpatch",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/newtest/dev/xvdf"},
				"data-in-memory": true,
			},
		},
	},
}

// mem to file/dev
var baseMap4 = map[string]interface{}{
	"namespaces": []interface{}{
		// for memory type to file type change
		map[string]interface{}{
			"name":               "memtofile",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
		// for memory type to device type change
		map[string]interface{}{
			"name":               "memtodev",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine":     "memory",
		},
	},
}
var patchMap4 = map[string]interface{}{
	"namespaces": []interface{}{
		// for memory type to file type change
		map[string]interface{}{
			"name": "memtofile",
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// for memory type to device type change
		map[string]interface{}{
			"name": "memtodev",
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/test/dev/xvdf"},
				"data-in-memory": true,
			},
		},
	},
}
var mergedMap4 = map[string]interface{}{
	"namespaces": []interface{}{
		// for memory type to file type change
		map[string]interface{}{
			"name":               "memtofile",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"files":          []interface{}{"test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
		// for memory type to device type change
		map[string]interface{}{
			"name":               "memtodev",
			"filesize":           2000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"devices":        []interface{}{"/test/dev/xvdf"},
				"data-in-memory": true,
			},
		},
	},
}

func TestConfigMerge(t *testing.T) {
	mergeAndCheck(t, baseMap1, patchMap1, mergedMap1)
	mergeAndCheck(t, baseMap2, patchMap2, mergedMap2)
	mergeAndCheck(t, baseMap3, patchMap3, mergedMap3)
	mergeAndCheck(t, baseMap4, patchMap4, mergedMap4)
}

func mergeAndCheck(t *testing.T, baseM, patchM, mergedM map[string]interface{}) {
	m, err := merge(baseM, patchM)
	if err != nil {
		t.Fatalf("failed to merge aerospike config baseMap and patchMap: %v", err)
	}

	if !reflect.DeepEqual(m, mergedM) {
		t.Fatalf("Merge failed, \nexpected %v, \ngot %v", mergedM, m)
	}
	t.Logf("AerospikeConfig merged successfully %v", m)
}
