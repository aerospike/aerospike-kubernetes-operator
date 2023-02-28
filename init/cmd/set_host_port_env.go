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

var hostIP string

// setIPEnv represents the setIPEnv command
var setIPEnv = &cobra.Command{
	Use:   "set-ip-env",
	Short: "This gets port and IP address info from cluster",
	Long: `This command fetch tls and info ports from services and
fetch internal and external IP from host and export to the desired
environment variables.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return pkg.SetHostPortEnv(k8sClient, podName, namespace, hostIP)
	},
}

func init() {
	rootCmd.AddCommand(setIPEnv)

	setIPEnv.Flags().StringVar(&hostIP, "host-ip", "", "Host ip on which current pod is running")
}
