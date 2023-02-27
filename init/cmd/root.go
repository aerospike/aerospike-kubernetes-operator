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
	"os"

	"github.com/spf13/cobra"
)

var (
	podName     string
	namespace   string
	clusterName string
)

var rootCmd = &cobra.Command{
	Use:   "akoinit",
	Short: "A library for init container",
	Long:  `A go library to perform certain init operations`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&podName, "pod-name", "p", "", "Current pod name")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace under which cluster is running")
	rootCmd.PersistentFlags().StringVarP(&clusterName, "cluster-name", "c", "", "Aerospike cluster name")
}
