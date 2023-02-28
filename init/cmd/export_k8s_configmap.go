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
	toDir  string
	cmName string
)

// exportK8sConfigmap represents the exportK8sConfigmap command
var exportK8sConfigmap = &cobra.Command{
	Use:   "export-configmap",
	Short: "Exports Kubernetes configmap to a directory",
	Long: `Exports Kubernetes configmap to a directory from
within a Kubernetes container to a directory as files.
The config map keys are the filenames with corresponding
values as the file content.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return pkg.ExportK8sConfigmap(k8sClient, namespace, cmName, toDir)
	},
}

func init() {
	rootCmd.AddCommand(exportK8sConfigmap)
	exportK8sConfigmap.Flags().StringVar(&toDir, "toDir", "", "Directory to which configmap is exported")
	exportK8sConfigmap.Flags().StringVar(&cmName, "cmName", "", "Configmap name that needs to be exported")
}
