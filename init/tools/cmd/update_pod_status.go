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
	podName     *string
	namespace   *string
	clusterName *string
	restartType *string
)

// updatePodStatus represents the updatePodStatus command
var updatePodStatus = &cobra.Command{
	Use:   "updatePodStatus",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return pkg.ManageVolumesAndUpdateStatus(podName, namespace, clusterName, restartType)
	},
}

func init() {
	rootCmd.AddCommand(updatePodStatus)
	podName = updatePodStatus.Flags().String("pod-name", "", "pod name")
	namespace = updatePodStatus.Flags().String("namespace", "", "namespace")
	clusterName = updatePodStatus.Flags().String("cluster-name", "", "cluster name")
	restartType = updatePodStatus.Flags().String("restart-type", "", "restart type")
}
