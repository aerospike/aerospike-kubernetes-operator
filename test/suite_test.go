/*
Copyright 2021.

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

package test

import (
	goctx "context"
	"flag"
	"fmt"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config

var testEnv *envtest.Environment

var k8sClient client.Client

var k8sClientset *kubernetes.Clientset

var (
	scheme = k8Runtime.NewScheme()
)

var defaultNetworkType = flag.String("connect-through-network-type", "hostExternal", "Network type is used to determine an appropriate access type. Can be 'pod', 'hostInternal' or 'hostExternal'. AS client in the test will choose access type witch matches expected network type. See details in https://docs.aerospike.com/docs/cloud/kubernetes/operator/Cluster-configuration-settings.html#network-policy")

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	junitReporter := reporters.NewJUnitReporter("junit.xml")
	RunSpecsWithDefaultAndCustomReporters(
		t,
		"Controller Suite",
		[]Reporter{junitReporter},
	)
}

var _ = BeforeEach(func() {
	By(fmt.Sprintf("Cleaning up all Aerospike clusters."))
	deleteAllClusters(namespace)
	deleteAllClusters(multiClusterNs1)
	deleteAllClusters(multiClusterNs2)
	err := cleanupPVC(k8sClient, namespace)
	Expect(err).NotTo(HaveOccurred())
	err = cleanupPVC(k8sClient, multiClusterNs1)
	Expect(err).NotTo(HaveOccurred())
	err = cleanupPVC(k8sClient, multiClusterNs2)
	Expect(err).NotTo(HaveOccurred())
})

func deleteAllClusters(namespace string) {
	ctx := goctx.TODO()
	list := &asdbv1beta1.AerospikeClusterList{}
	listOps := &client.ListOptions{Namespace: namespace}

	err := k8sClient.List(ctx, list, listOps)
	Expect(err).NotTo(HaveOccurred())

	for _, cluster := range list.Items {
		By(fmt.Sprintf("Deleting cluster \"%s/%s\".", cluster.Namespace, cluster.Name))
		err := deleteCluster(k8sClient, ctx, &cluster)
		Expect(err).NotTo(HaveOccurred())
	}
}

func cleanupPVC(k8sClient client.Client, ns string) error {
	// t.Log("Cleanup old pvc")

	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	clLabels := map[string]string{"app": "aerospike-cluster"}
	labelSelector := labels.SelectorFromSet(clLabels)
	listOps := &client.ListOptions{Namespace: ns, LabelSelector: labelSelector}

	if err := k8sClient.List(goctx.TODO(), pvcList, listOps); err != nil {
		return err
	}

	for _, pvc := range pvcList.Items {
		pkgLog.Info("Found pvc, deleting it", "pvcName", pvc.Name, "namespace", pvc.Namespace)

		if utils.IsPVCTerminating(&pvc) {
			continue
		}

		if utils.ContainsString(pvc.Finalizers, "kubernetes.io/pvc-protection") {
			pvc.Finalizers = utils.RemoveString(pvc.Finalizers, "kubernetes.io/pvc-protection")
			if err := k8sClient.Update(goctx.TODO(), &pvc); err != nil {
				return fmt.Errorf("could not remove %s finalizer from following pvc: %s: %w",
					"kubernetes.io/pvc-protection", pvc.Name, err)
			}
		}

		if err := k8sClient.Delete(goctx.TODO(), &pvc); err != nil {
			return fmt.Errorf("could not delete pvc %s: %w", pvc.Name, err)
		}
	}
	return nil
}

// This is used when running tests on existing cluster
// user has to install its own operator then run cleanup and then start this

var _ = BeforeSuite(
	func(done Done) {
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")
		pkgLog.Info(fmt.Sprintf("Client will connect throug '%s' network to Aerospike Clusters.", *defaultNetworkType))
		t := true
		testEnv = &envtest.Environment{
			UseExistingCluster: &t,
		}
		var err error

		cfg, err = testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		err = clientgoscheme.AddToScheme(clientgoscheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		err = asdbv1beta1.AddToScheme(clientgoscheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		err = admissionv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		// +kubebuilder:scaffold:scheme

		k8sClient, err = client.New(
			cfg, client.Options{Scheme: clientgoscheme.Scheme},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		k8sClientset = kubernetes.NewForConfigOrDie(cfg)
		Expect(k8sClient).NotTo(BeNil())

		ctx := goctx.TODO()
		_ = createNamespace(k8sClient, ctx, namespace)

		// Setup by user function
		// test creating resource
		// IN operator namespace
		// Create aerospike-secret
		// Create auth-secret (admin)
		// Create auth-update (admin123)

		// For test1
		// Create aerospike-secret
		// Create auth-secret (admin)

		// For test2
		// Create aerospike-secret
		// Create auth-secret (admin)

		// For common
		// Create namespace test1, test2
		// ServiceAccount: aerospike-cluster (operatorNs, test1, test2)
		// ClusterRole: aerospike-cluster
		// ClusterRoleBinding: aerospike-cluster

		// Need to create storageclass if not created already

		err = setupByUser(k8sClient, ctx)
		Expect(err).ToNot(HaveOccurred())

		close(done)
	}, 120,
)

var _ = AfterSuite(
	func() {
		By("Cleaning up all pvcs")
		_ = cleanupPVC(k8sClient, namespace)
		_ = cleanupPVC(k8sClient, multiClusterNs1)
		_ = cleanupPVC(k8sClient, multiClusterNs2)

		By("tearing down the test environment")
		gexec.KillAndWait(5 * time.Second)
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
