package v1

import (
	"testing"
)

// TestGetIndexCheckpointNamespaces pins the set to the service-level path plus
// skip-checkpoint, and pins that it may be wider than what the server checkpoints.
func TestGetIndexCheckpointNamespaces(t *testing.T) {
	shmemNS := func(name string, extra map[string]interface{}) map[string]interface{} {
		ns := map[string]interface{}{
			"name":           name,
			"storage-engine": map[string]interface{}{"type": "memory"},
		}
		for k, v := range extra {
			ns[k] = v
		}

		return ns
	}
	config := func(path string, nss ...interface{}) map[string]interface{} {
		conf := map[string]interface{}{"namespaces": nss}
		if path != "" {
			conf["service"] = map[string]interface{}{"index-checkpoint-path": path}
		}

		return conf
	}

	tests := []struct {
		config   map[string]interface{}
		name     string
		expected []string
	}{
		{
			name:     "no service path: feature off, nothing checkpoints",
			config:   config("", shmemNS("test", nil)),
			expected: nil,
		},
		{
			name:     "empty service path is the same as unset",
			config:   config("", shmemNS("test", nil)),
			expected: nil,
		},
		{
			name:     "global path: every eligible namespace inherits it",
			config:   config("/mnt/ckpt", shmemNS("a", nil), shmemNS("b", nil)),
			expected: []string{"a", "b"},
		},
		{
			name: "skip-checkpoint opts a namespace out",
			config: config("/mnt/ckpt",
				shmemNS("a", nil),
				shmemNS("b", map[string]interface{}{"skip-checkpoint": true}),
			),
			expected: []string{"a"},
		},
		{
			name: "skip-checkpoint false leaves it opted in",
			config: config("/mnt/ckpt",
				shmemNS("a", map[string]interface{}{"skip-checkpoint": false}),
			),
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetIndexCheckpointNamespaces(tt.config)
			if len(result) != len(tt.expected) {
				t.Fatalf("GetIndexCheckpointNamespaces() = %v, expected %v", result, tt.expected)
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Fatalf("GetIndexCheckpointNamespaces() = %v, expected %v", result, tt.expected)
				}
			}
		})
	}
}

// TestGetIndexCheckpointPath pins that the path is read from the service section only.
func TestGetIndexCheckpointPath(t *testing.T) {
	tests := []struct {
		config   map[string]interface{}
		name     string
		expected string
	}{
		{
			name: "set in the service section",
			config: map[string]interface{}{
				"service": map[string]interface{}{"index-checkpoint-path": "/mnt/ckpt"},
			},
			expected: "/mnt/ckpt",
		},
		{
			name:     "no service section",
			config:   map[string]interface{}{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := GetIndexCheckpointPath(tt.config); result != tt.expected {
				t.Errorf("GetIndexCheckpointPath() = %q, expected %q", result, tt.expected)
			}
		})
	}
}
