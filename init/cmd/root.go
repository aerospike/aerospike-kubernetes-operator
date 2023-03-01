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
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

var (
	podName   string
	namespace string
	k8sClient client.Client
)

var rootCmd = &cobra.Command{
	Use:   "akoinit",
	Short: "A library for init container",
	Long: `A go library to perform certain init operations like
exporting configmap to file, updating pod status in cluster CR,
getting host port and IP addresses`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&podName, "pod-name", "", "Current pod name")
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace under which cluster is running")

	var err error

	cfg := ctrl.GetConfigOrDie()

	if err = clientgoscheme.AddToScheme(clientgoscheme.Scheme); err != nil {
		panic(err.Error())
	}

	if err = asdbv1beta1.AddToScheme(clientgoscheme.Scheme); err != nil {
		panic(err.Error())
	}

	if k8sClient, err = client.New(
		cfg, client.Options{Scheme: clientgoscheme.Scheme},
	); err != nil {
		panic(err.Error())
	}
}
