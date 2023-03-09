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

var (
	cmName      string
	cmNamespace string
)

// quickRestart represents the quick-restart command
var quickRestart = &cobra.Command{
	Use:   "quick-restart",
	Short: "quick restart init functionality",
	Long: `This command runs quick restart functionality like
restart asd process in server and updating CR status.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		initp, err := pkg.PopulateInitParams()
		if err != nil {
			return err
		}
		return initp.QuickRestart(cmName, cmNamespace)
	},
}

func init() {
	rootCmd.AddCommand(quickRestart)
	quickRestart.Flags().StringVar(&cmName, "cm-name", "", "configmap name")
	quickRestart.Flags().StringVar(&cmNamespace, "cm-namespace", "", "configmap namespace")
}
