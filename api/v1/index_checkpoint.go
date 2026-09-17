/*
Copyright 2024.

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

package v1

import (
	"strings"
)

// GetIndexCheckpointPath returns the cluster-wide index-checkpoint-path from the
// SERVICE section, or "" if the feature is off.
func GetIndexCheckpointPath(aerospikeConfig map[string]interface{}) string {
	serviceConf, ok := aerospikeConfig[ConfKeyService].(map[string]interface{})
	if !ok {
		return ""
	}

	path, _ := serviceConf[ConfKeyServiceIndexCheckpointPath].(string)

	return path
}

// IsCheckpointSkipped reports whether a namespace opted out with skip-checkpoint.
// Anything other than boolean true leaves it opted in, matching the server's default.
func IsCheckpointSkipped(namespaceConf map[string]interface{}) bool {
	skip, _ := namespaceConf[ConfKeySkipCheckpoint].(bool)

	return skip
}

// IsCheckpointUsableNamespaceName mirrors the server's namespace-name rule. It builds
// "<path>/<name>" plus ".tmp" (save staging) and ".deleting" (go-live delete in
// progress) siblings, so these names escape the checkpoint directory or collide with a
// sibling's, and it crashes at startup on them.
func IsCheckpointUsableNamespaceName(name string) bool {
	return name != "." && name != ".." &&
		!strings.Contains(name, "/") &&
		!strings.HasSuffix(name, ".tmp") &&
		!strings.HasSuffix(name, ".deleting")
}

// GetIndexCheckpointNamespaces returns the namespaces that may need a checkpoint-save
// before their pod is deleted: the cluster-wide path is set and skip-checkpoint is not.
func GetIndexCheckpointNamespaces(aerospikeConfig map[string]interface{}) []string {
	if GetIndexCheckpointPath(aerospikeConfig) == "" {
		return nil
	}

	namespaces, ok := aerospikeConfig[ConfKeyNamespace].([]interface{})
	if !ok {
		return nil
	}

	var result []string

	for _, ns := range namespaces {
		nsConf, ok := ns.(map[string]interface{})
		if !ok {
			continue
		}

		if IsCheckpointSkipped(nsConf) {
			continue
		}

		if name, ok := nsConf[ConfKeyName].(string); ok && name != "" {
			result = append(result, name)
		}
	}

	return result
}
