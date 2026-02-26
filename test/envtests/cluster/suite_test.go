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

package cluster

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	envtests "github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
)

func TestClusterWebhooks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Webhook Envtests Suite")
}

var _ = BeforeSuite(func() {
	// basePath from test/envtests/cluster to repo root
	envtests.SetupTestEnv("../../../")
})

var _ = AfterSuite(func() {
	envtests.TeardownTestEnv()
})
