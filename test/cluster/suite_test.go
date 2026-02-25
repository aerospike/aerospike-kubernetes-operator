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
	"bytes"
	goctx "context"
	"encoding/gob"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var testEnv *envtest.Environment

var k8sClient client.Client

var cfg *rest.Config

var k8sClientSet *kubernetes.Clientset

var projectRoot string

var scheme = k8Runtime.NewScheme()

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Suite")
}

func deleteAllClusters(namespace string) {
	ctx := goctx.TODO()
	list := &asdbv1.AerospikeClusterList{}
	listOps := &client.ListOptions{Namespace: namespace}

	err := k8sClient.List(ctx, list, listOps)
	Expect(err).NotTo(HaveOccurred())

	for clusterIndex := range list.Items {
		By(fmt.Sprintf("Deleting cluster \"%s/%s\".", list.Items[clusterIndex].Namespace, list.Items[clusterIndex].Name))
		err := DeleteCluster(k8sClient, ctx, &list.Items[clusterIndex])
		Expect(err).NotTo(HaveOccurred())
	}
}

// This is used when running tests on existing cluster
// user has to install its own operator then run cleanup and then start this

var _ = SynchronizedBeforeSuite(
	func() []byte {
		var err error

		testEnv, cfg, err = test.StartTestEnvironment()
		Expect(err).NotTo(HaveOccurred())

		k8sClient, _, err = test.InitialiseClients(scheme, cfg)
		Expect(err).NotTo(HaveOccurred())

		// Set up all necessary Secrets, RBAC roles, and ServiceAccounts for the test environment
		err = test.SetupByUser(k8sClient, goctx.TODO())
		Expect(err).ToNot(HaveOccurred())

		var buf bytes.Buffer

		enc := gob.NewEncoder(&buf)
		Expect(enc.Encode(cfg)).To(Succeed())

		return buf.Bytes()
	},

	func(data []byte) {
		// this runs once per process, we grab the existing rest.Config here
		dec := gob.NewDecoder(bytes.NewReader(data))

		var (
			config rest.Config
			err    error
		)
		Expect(dec.Decode(&config)).To(Succeed())
		cfg = &config

		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")
		pkgLog.Info(fmt.Sprintf("Client will connect through '%s' network to Aerospike Clusters.",
			*defaultNetworkType))

		k8sClient, k8sClientSet, err = test.InitialiseClients(scheme, cfg)
		Expect(err).NotTo(HaveOccurred())

		projectRoot, err = getGitRepoRootPath()
		Expect(err).NotTo(HaveOccurred())

		cloudProvider, err = getCloudProvider(goctx.TODO(), k8sClient)
		Expect(err).ToNot(HaveOccurred())
	},
)

var _ = SynchronizedAfterSuite(func() {
	// runs on *all* processes
}, func() {
	// runs *only* on process #1
	By("Cleaning up all clusters and pvcs")

	for idx := range test.Namespaces {
		deleteAllClusters(test.Namespaces[idx])
		_ = CleanupPVC(k8sClient, test.Namespaces[idx], "")
	}

	By("tearing down the test environment")
	gexec.KillAndWait(5 * time.Second)

	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
