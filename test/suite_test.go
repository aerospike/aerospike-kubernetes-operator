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
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	// "k8s.io/client-go/kubernetes/scheme"
	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"

	// utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	admissionv1 "k8s.io/api/admission/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config

// var k8sClient client.Client
var testEnv *envtest.Environment

// var ctx goctx.Context
var k8sClient client.Client

var (
	setupLog = ctrl.Log.WithName("setup")
	scheme   = k8Runtime.NewScheme()
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	junitReporter := reporters.NewJUnitReporter("junit.xml")
	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{junitReporter, printer.NewlineReporter{}},
	)
}

// This is used when running tests on existing cluster
// user has to install its own operator then run cleanup and then start this

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
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

	err = asdbv1alpha1.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = admissionv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Cleanup old objects
	// ot, err := exec.Command("/bin/sh", "test/test/cleanup-test-namespace.sh").Output()
	// // Expect(err).ToNot(HaveOccurred())
	// setupLog.Info("Clenup old objects", "output", string(ot))

	ctx := goctx.TODO()
	createNamespace(k8sClient, ctx, namespace)

	err = setupByUser(k8sClient, ctx)
	Expect(err).ToNot(HaveOccurred())

	close(done)
}, 120)

// var _ = BeforeSuite(func(done Done) {
// 	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

// 	By("bootstrapping test environment")
// 	// t := true
// 	// if os.Getenv("TEST_USE_EXISTING_CLUSTER") == "true" {
// 	// 	testEnv = &envtest.Environment{
// 	// 		UseExistingCluster: &t,
// 	// 	}
// 	// } else {
// 	testEnv = &envtest.Environment{
// 		// UseExistingCluster: &t,
// 		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")},
// 		WebhookInstallOptions: envtest.WebhookInstallOptions{
// 			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
// 		},
// 	}
// 	// }
// 	var err error

// 	cfg, err = testEnv.Start()
// 	Expect(err).NotTo(HaveOccurred())
// 	Expect(cfg).NotTo(BeNil())

// 	err = clientgoscheme.AddToScheme(clientgoscheme.Scheme)
// 	Expect(err).NotTo(HaveOccurred())

// 	err = asdbv1alpha1.AddToScheme(clientgoscheme.Scheme)
// 	Expect(err).NotTo(HaveOccurred())

// 	err = admissionv1.AddToScheme(scheme)
// 	Expect(err).NotTo(HaveOccurred())

// 	// +kubebuilder:scaffold:scheme

// 	k8sClient, err = client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
// 	Expect(err).NotTo(HaveOccurred())
// 	Expect(k8sClient).NotTo(BeNil())

// 	webhookInstallOptions := &testEnv.WebhookInstallOptions

// 	// Create a new Cmd to provide shared dependencies and start components
// 	options := ctrl.Options{
// 		ClientBuilder: &newClientBuilder{},
// 		Scheme:        clientgoscheme.Scheme,
// 		// Port:          9443,
// 		Namespace:      namespace,
// 		Host:           webhookInstallOptions.LocalServingHost,
// 		Port:           webhookInstallOptions.LocalServingPort,
// 		CertDir:        webhookInstallOptions.LocalServingCertDir,
// 		LeaderElection: false,
// 		// MetricsBindAddress: "0",

// 	}

// 	k8sManager, err := ctrl.NewManager(cfg, options)
// 	Expect(err).ToNot(HaveOccurred())

// 	setupLog.Info("Init aerospike-server config schemas")
// 	asconfig.InitFromMap(configschema.SchemaMap)

// 	err = (&aerospikecluster.AerospikeClusterReconciler{
// 		Client: k8sManager.GetClient(),
// 		Scheme: k8sManager.GetScheme(),
// 		Log:    ctrl.Log.WithName("controllers").WithName("AerospikeCluster"),
// 	}).SetupWithManager(k8sManager)
// 	Expect(err).ToNot(HaveOccurred())

// 	err = (&asdbv1alpha1.AerospikeCluster{}).SetupWebhookWithManager(k8sManager)
// 	Expect(err).ToNot(HaveOccurred())

// 	setupLog.Info("starting manager")
// 	go func() {
// 		err = k8sManager.Start(ctrl.SetupSignalHandler())
// 		setupLog.Info("@@@@@@@@@@@@@@@@@@@@@@@ err", "err", err)

// 		Expect(err).ToNot(HaveOccurred())
// 	}()

// 	// // k8sClient = k8sManager.GetClient()
// 	// // Expect(k8sClient).ToNot(BeNil())

// 	// wait for the webhook server to get ready
// 	dialer := &net.Dialer{Timeout: time.Second}
// 	// svr := k8sManager.GetWebhookServer()
// 	// addrPort := fmt.Sprintf("%s:%d", svr.Host, svr.Port)
// 	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
// 	// addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, 9443)
// 	Eventually(func() error {
// 		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true})
// 		if err != nil {
// 			return err
// 		}
// 		conn.Close()
// 		return nil
// 	}).Should(Succeed())

// 	// Cleanup old objects
// 	// ot, err := exec.Command("/bin/sh", "test/test/cleanup-test-namespace.sh").Output()
// 	// // Expect(err).ToNot(HaveOccurred())
// 	// setupLog.Info("Clenup old objects", "output", string(ot))

// 	ctx := goctx.TODO()
// 	createNamespace(k8sClient, ctx, namespace)

// 	err = setupByUser(k8sClient, ctx)
// 	Expect(err).ToNot(HaveOccurred())

// 	nodeList := &corev1.NamespaceList{}
// 	err = k8sClient.List(goctx.Background(), nodeList)
// 	Expect(err).ToNot(HaveOccurred())

// 	for _, p := range nodeList.Items {
// 		setupLog.Info(" @@@@@@@Node ", "node", p.Name)
// 	}

// 	// podList := &corev1.PodList{}
// 	// listOps := &client.ListOptions{Namespace: "kube-system"}
// 	// err = k8sClient.List(goctx.Background(), podList, listOps)
// 	// Expect(err).ToNot(HaveOccurred())

// 	// setupLog.Info("@@@@@@@@@@@@@@@@@@@@@@@ print pod", "pod", podList.Items)
// 	// for _, p := range podList.Items {
// 	// 	setupLog.Info("Pod %v", "pod", p)
// 	// }

// 	close(done)
// }, 120)

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

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	gexec.KillAndWait(5 * time.Second)
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

type newClientBuilder struct{}

func (n *newClientBuilder) WithUncached(objs ...client.Object) manager.ClientBuilder {
	// n.uncached = append(n.uncached, objs...)
	return n
}

func (n *newClientBuilder) Build(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error) {
	// Create the Client for Write operations.
	return client.New(config, options)
}

//**********************************************************************************************

// var _ = Describe("CronJob controller2", func() {

// 	Context("When updating CronJob Status", func() {
// 		It("Deploying the cluster", func() {
// 			ctx := goctx.TODO()
// 			clusterName := "podspec"
// 			clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

// 			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
// 			By("Deploying the cluster")

// 			err := deployCluster(k8sClient, ctx, aeroCluster)
// 			// err := k8sClient.Create(ctx, aeroCluster)

// 			Expect(err).ToNot(HaveOccurred())
// 			// time.Sleep(time.Second * 5)
// 		})
// 	})
// })
