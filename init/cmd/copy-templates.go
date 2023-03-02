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
	source      string
	destination string
)

// exportK8sConfigmap represents the exportK8sConfigmap command
var copyTemplates = &cobra.Command{
	Use:   "copy-templates",
	Short: "Copy template files from source to destination",
	Long: `Copy aerospike.template.conf file and peers from given 
source to destination.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return pkg.CopyTemplates(source, destination)
	},
}

func init() {
	rootCmd.AddCommand(copyTemplates)
	copyTemplates.Flags().StringVar(&source, "source", "", "Directory from which template files needs to be copied")
	copyTemplates.Flags().StringVar(&destination, "destination", "", "Directory to which template files needs to be copied")
}
