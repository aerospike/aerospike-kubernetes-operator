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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config

var testEnv *envtest.Environment

var k8sClient client.Client

var k8sClientset *kubernetes.Clientset

var cloudProvider CloudProvider

var projectRoot string

var (
	scheme = k8Runtime.NewScheme()
)

var defaultNetworkType = flag.String("connect-through-network-type", "hostExternal",
	"Network type is used to determine an appropriate access type. Can be 'pod',"+
		" 'hostInternal' or 'hostExternal'. AS client in the test will choose access type"+
		" which matches expected network type. See details in"+
		" https://docs.aerospike.com/docs/cloud/kubernetes/operator/Cluster-configuration-settings.html#network-policy")

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeEach(func() {
	By("Cleaning up all Aerospike clusters.")

	for idx := range testNamespaces {
		deleteAllClusters(testNamespaces[idx])
		Expect(cleanupPVC(k8sClient, testNamespaces[idx])).NotTo(HaveOccurred())
	}
})

func deleteAllClusters(namespace string) {
	ctx := goctx.TODO()
	list := &asdbv1.AerospikeClusterList{}
	listOps := &client.ListOptions{Namespace: namespace}

	err := k8sClient.List(ctx, list, listOps)
	Expect(err).NotTo(HaveOccurred())

	for clusterIndex := range list.Items {
		By(fmt.Sprintf("Deleting cluster \"%s/%s\".", list.Items[clusterIndex].Namespace, list.Items[clusterIndex].Name))
		err := deleteCluster(k8sClient, ctx, &list.Items[clusterIndex])
		Expect(err).NotTo(HaveOccurred())
	}
}

func cleanupPVC(k8sClient client.Client, ns string) error {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	clLabels := map[string]string{"app": "aerospike-cluster"}
	labelSelector := labels.SelectorFromSet(clLabels)
	listOps := &client.ListOptions{Namespace: ns, LabelSelector: labelSelector}

	if err := k8sClient.List(goctx.TODO(), pvcList, listOps); err != nil {
		return err
	}

	for pvcIndex := range pvcList.Items {
		pkgLog.Info("Found pvc, deleting it", "pvcName",
			pvcList.Items[pvcIndex].Name, "namespace", pvcList.Items[pvcIndex].Namespace)

		if utils.IsPVCTerminating(&pvcList.Items[pvcIndex]) {
			continue
		}
		// if utils.ContainsString(pvc.Finalizers, "kubernetes.io/pvc-protection") {
		//	pvc.Finalizers = utils.RemoveString(pvc.Finalizers, "kubernetes.io/pvc-protection")
		//	if err := k8sClient.Patch(goctx.TODO(), &pvc, client.Merge); err != nil {
		//		return fmt.Errorf("could not patch %s finalizer from following pvc: %s: %w",
		//			"kubernetes.io/pvc-protection", pvc.Name, err)
		//	}
		//}
		if err := k8sClient.Delete(goctx.TODO(), &pvcList.Items[pvcIndex]); err != nil {
			return fmt.Errorf("could not delete pvc %s: %w", pvcList.Items[pvcIndex].Name, err)
		}
	}

	return nil
}

func deletePVC(k8sClient client.Client, pvcNamespacedName types.NamespacedName) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := k8sClient.Get(goctx.TODO(), pvcNamespacedName, pvc); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}

		return err
	}

	if utils.IsPVCTerminating(pvc) {
		return nil
	}

	if err := k8sClient.Delete(goctx.TODO(), pvc); err != nil {
		return fmt.Errorf("could not delete pvc %s: %w", pvc.Name, err)
	}

	return nil
}

// This is used when running tests on existing cluster
// user has to install its own operator then run cleanup and then start this

var _ = BeforeSuite(
	func() {
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")
		pkgLog.Info(fmt.Sprintf("Client will connect through '%s' network to Aerospike Clusters.", *defaultNetworkType))
		t := true
		testEnv = &envtest.Environment{
			UseExistingCluster: &t,
		}
		var err error

		cfg, err = testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		err = clientgoscheme.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		err = asdbv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		err = admissionv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		// +kubebuilder:scaffold:scheme

		k8sClient, err = client.New(
			cfg, client.Options{Scheme: scheme},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		k8sClientset = kubernetes.NewForConfigOrDie(cfg)
		Expect(k8sClient).NotTo(BeNil())

		projectRoot, err = getGitRepoRootPath()
		Expect(err).NotTo(HaveOccurred())

		ctx := goctx.TODO()

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

		// For aerospike
		// Create aerospike-secret
		// Create auth-secret (admin)

		// For common
		// Create namespace test1, test2, aerospike
		// ServiceAccount: aerospike-cluster (operatorNs, test1, test2, aerospike)
		// ClusterRole: aerospike-cluster
		// ClusterRoleBinding: aerospike-cluster

		// Need to create storageClass if not created already
		err = setupByUser(k8sClient, ctx)
		Expect(err).ToNot(HaveOccurred())
		cloudProvider, err = getCloudProvider(ctx, k8sClient)
		Expect(err).ToNot(HaveOccurred())
	})

var _ = AfterSuite(
	func() {
		By("Cleaning up all pvcs")

		for idx := range testNamespaces {
			_ = cleanupPVC(k8sClient, testNamespaces[idx])
		}

		By("tearing down the test environment")
		gexec.KillAndWait(5 * time.Second)
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
