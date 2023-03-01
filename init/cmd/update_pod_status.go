/*
Copyright 2023 The aerospike-operator Authors.
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

package cmd

import (
	"github.com/spf13/cobra"

	"create-podstatus/pkg"
)

var (
	restartType string
	clusterName string
)

// updatePodStatus represents the updatePodStatus command
var updatePodStatus = &cobra.Command{
	Use:   "update-pod-status",
	Short: "Handle volume operations and update pod status in aerospike cluster",
	Long: `This command initialize, wipe and clean the dirty volumes
based on the use case and update pod status in aerospike cluster`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return pkg.ManageVolumesAndUpdateStatus(k8sClient, podName, namespace, clusterName, restartType)
	},
}

func init() {
	rootCmd.AddCommand(updatePodStatus)

	updatePodStatus.Flags().StringVar(&restartType, "restart-type", "podRestart",
		"Can either be quickRestart or podRestart")
	updatePodStatus.Flags().StringVar(&clusterName, "cluster-name", "", "Aerospike cluster name")
}
