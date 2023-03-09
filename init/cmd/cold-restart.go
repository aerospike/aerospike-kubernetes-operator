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

	"akoinit/pkg"
)

// coldRestart represents the cold-restart command
var coldRestart = &cobra.Command{
	Use:   "cold-restart",
	Short: "init container functionality",
	Long: `This command runs complete init container functionality
during pod start and subsequent cold restarts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		initp, err := pkg.PopulateInitParams()
		if err != nil {
			return err
		}
		return initp.ColdRestart()
	},
}

func init() {
	rootCmd.AddCommand(coldRestart)
}
