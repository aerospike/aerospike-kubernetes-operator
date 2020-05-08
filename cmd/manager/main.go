package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"github.com/citrusleaf/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/citrusleaf/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/citrusleaf/aerospike-kubernetes-operator/pkg/controller"
	ctrAdmission "github.com/citrusleaf/aerospike-kubernetes-operator/pkg/controller/admission"
	"github.com/citrusleaf/aerospike-kubernetes-operator/pkg/controller/configschema"
	"github.com/citrusleaf/aerospike-kubernetes-operator/version"
	"github.com/citrusleaf/aerospike-management-lib/asconfig"

	log "github.com/inconshreveable/log15"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	kubemetrics "github.com/operator-framework/operator-sdk/pkg/kube-metrics"
	"github.com/operator-framework/operator-sdk/pkg/leader"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	"github.com/operator-framework/operator-sdk/pkg/restmapper"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost               = "0.0.0.0"
	metricsPort         int32 = 8383
	operatorMetricsPort int32 = 8686

	// Webhook cert directory path
	certDir = "/tmp/cert"

	logger = log.New(log.Ctx{"module": "cmd"})
)

const (
	logLevelEnvVar   = "LOG_LEVEL"
	syncPeriodEnvVar = "SYNC_PERIOD_SECOND"
)

var mgr ctrl.Manager

var SchemeGroupVersion = schema.GroupVersion{Group: aerospikev1alpha1.SchemeGroupVersion.Group, Version: aerospikev1alpha1.SchemeGroupVersion.Version}

func addKnownTypes(scheme *k8Runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&aerospikev1alpha1.AerospikeCluster{},
	)
	k8v1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

func printVersion() {
	logger.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	logger.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	logger.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	logger.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

func getLogLevel() log.Lvl {
	logLevel, found := os.LookupEnv(logLevelEnvVar)
	if !found {
		return log.LvlInfo
	}
	level, err := log.LvlFromString(logLevel)
	if err != nil {
		return log.LvlInfo
	}
	return level
}

// levelFilterHandler filters log messages based on the current log level.
func levelFilterHandler(h log.Handler, logLevel log.Lvl) log.Handler {
	return log.FilterHandler(func(r *log.Record) (pass bool) {
		return r.Lvl <= logLevel
	}, h)
}

// setupLogger sets up the logger from the config.
func setupLogger() {
	handler := log.Root().GetHandler()
	// caller handler
	handler = log.CallerFileHandler(handler)

	handler = levelFilterHandler(handler, getLogLevel())

	log.Root().SetHandler(handler)
}

func getSyncPeriod() *time.Duration {
	sync, found := os.LookupEnv(syncPeriodEnvVar)
	if !found {
		return nil
	}
	syncPeriod, err := strconv.Atoi(sync)
	if err != nil || syncPeriod == 0 {
		return nil
	}
	d := time.Duration(syncPeriod) * time.Second
	return &d
}

func main() {

	setupLogger()

	printVersion()

	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		logger.Error("Failed to get watch namespace", log.Ctx{"err": err})
		os.Exit(1)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		logger.Error("Failed to init config", log.Ctx{"err": err})
		os.Exit(1)
	}

	ctx := context.TODO()
	// Become the leader before proceeding
	err = leader.Become(ctx, "aerospike-kubernetes-operator-lock")
	if err != nil {
		logger.Error("Failed to become leader", log.Ctx{"err": err})
		os.Exit(1)
	}

	scheme := k8Runtime.NewScheme()
	SchemeBuilder := k8Runtime.NewSchemeBuilder(addKnownTypes)
	if err := SchemeBuilder.AddToScheme(scheme); err != nil {
		logger.Error("Failed to add scheme", log.Ctx{"err": err})
		os.Exit(1)
	}
	err = corev1.AddToScheme(scheme)
	if err != nil {
		logger.Error("Failed to add scheme", log.Ctx{"err": err})
		os.Exit(1)
	}
	err = appsv1.AddToScheme(scheme)
	if err != nil {
		logger.Error("Failed to add scheme", log.Ctx{"err": err})
		os.Exit(1)
	}
	err = storagev1.AddToScheme(scheme)
	if err != nil {
		logger.Error("Failed to add scheme", log.Ctx{"err": err})
		os.Exit(1)
	}
	err = admissionregistrationv1beta1.AddToScheme(scheme)
	if err != nil {
		logger.Error("Failed to add scheme", log.Ctx{"err": err})
		os.Exit(1)
	}

	d := getSyncPeriod()
	logger.Info("Set sync period", log.Ctx{"period": d})

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err = manager.New(cfg, manager.Options{
		Scheme:             scheme,
		Namespace:          namespace,
		MapperProvider:     restmapper.NewDynamicRESTMapper,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		NewClient:          newClient,
		SyncPeriod:         d,
	})
	if err != nil {
		logger.Error("Failed to create manager", log.Ctx{"err": err})
		os.Exit(1)
	}

	logger.Info("Registering Components")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		logger.Error("Failed to add schemes", log.Ctx{"err": err})
		os.Exit(1)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		logger.Error("Failed to setup all controller", log.Ctx{"err": err})
		os.Exit(1)
	}

	if err = serveCRMetrics(cfg); err != nil {
		logger.Info("Could not generate and serve custom resource metrics", log.Ctx{"err": err})
	}

	// Add to the below struct any other metrics ports you want to expose.
	servicePorts := []corev1.ServicePort{
		{Port: metricsPort, Name: metrics.OperatorPortName, Protocol: corev1.ProtocolTCP, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: metricsPort}},
		{Port: operatorMetricsPort, Name: metrics.CRPortName, Protocol: corev1.ProtocolTCP, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: operatorMetricsPort}},
	}
	// Create Service object to expose the metrics port(s).
	_, err = metrics.CreateMetricsService(ctx, cfg, servicePorts)
	if err != nil {
		logger.Info("Could not create metrics Service", log.Ctx{"err": err})
	}

	// // CreateServiceMonitors will automatically create the prometheus-operator ServiceMonitor resources
	// // necessary to configure Prometheus to scrape metrics from this operator.
	// services := []*corev1.Service{service}
	// _, err = metrics.CreateServiceMonitors(cfg, namespace, services)
	// if err != nil {
	// 	logger.Info("Could not create ServiceMonitor object", log.Ctx{"err": err})
	// 	// If this operator is deployed to a cluster without the prometheus-operator running, it will return
	// 	// ErrServiceMonitorNotPresent, which can be used to safely skip ServiceMonitor creation.
	// 	if err == metrics.ErrServiceMonitorNotPresent {
	// 		logger.Info("Install prometheus-operator in your cluster to create ServiceMonitor objects", log.Ctx{"err": err})
	// 	}
	// }

	logger.Info("Setup secret for admission webhook")
	err = ctrAdmission.SetupSecret(certDir, namespace)
	if err != nil {
		logger.Error("Failed to setup secret for webhook", log.Ctx{"err": err})
		os.Exit(1)
	}

	logger.Info("Setup webhook")
	if err := setupWebhookServer(cfg, namespace); err != nil {
		logger.Error("Failed to setup admission webhook server", log.Ctx{"err": err})
		os.Exit(1)
	}

	logger.Info("Init aerospike-server config schemas")
	asconfig.InitFromMap(configschema.SchemaMap)

	logger.Info("Start the Cmd")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		logger.Error("Manager exited non-zero", log.Ctx{"err": err})
		os.Exit(1)
	}
}

// newClient creates the default caching client
// this will read/write directly from api-server
func newClient(cache cache.Cache, config *rest.Config, options crclient.Options) (crclient.Client, error) {
	// Create the Client for Write operations.
	return crclient.New(config, options)
}

// serveCRMetrics gets the Operator/CustomResource GVKs and generates metrics based on those types.
// It serves those metrics on "http://metricsHost:operatorMetricsPort".
func serveCRMetrics(cfg *rest.Config) error {
	// Below function returns filtered operator/CustomResource specific GVKs.
	// For more control override the below GVK list with your own custom logic.
	filteredGVK, err := k8sutil.GetGVKsFromAddToScheme(apis.AddToScheme)
	if err != nil {
		return err
	}
	// Get the namespace the operator is currently deployed in.
	operatorNs, err := k8sutil.GetOperatorNamespace()
	if err != nil {
		return err
	}
	// To generate metrics in other namespaces, add the values below.
	ns := []string{operatorNs}
	// Generate and serve custom resource specific metrics.
	err = kubemetrics.GenerateAndServeCRMetrics(cfg, ns, filteredGVK, metricsHost, operatorMetricsPort)
	if err != nil {
		return err
	}
	return nil
}

func setupWebhookServer(cfg *rest.Config, namespace string) error {

	logger.Info("Add validating webhook handler")
	validatingHook := &webhook.Admission{
		Handler: admission.HandlerFunc(func(ctx context.Context, req webhook.AdmissionRequest) webhook.AdmissionResponse {
			return ctrAdmission.ValidateAerospikeCluster(req)
		}),
	}

	logger.Info("Add mutation webhook handler")
	mutatingHook := &webhook.Admission{
		Handler: admission.HandlerFunc(func(ctx context.Context, req webhook.AdmissionRequest) webhook.AdmissionResponse {
			return ctrAdmission.MutateAerospikeCluster(req)
		}),
	}

	logger.Info("Create webhook server")
	hookServer := &webhook.Server{
		Port:    8443,
		CertDir: certDir,
	}
	if err := mgr.Add(hookServer); err != nil {
		return err
	}

	hookServer.Register(ctrAdmission.AerospikeClusterValidationWebhookPath, validatingHook)
	hookServer.Register(ctrAdmission.AerospikeClusterMutationWebhookPath, mutatingHook)

	logger.Info("Register validation webhook")
	wh1 := ctrAdmission.NewValidatingAdmissionWebhook(namespace, mgr, mgr.GetClient())
	if err := wh1.Register(certDir); err != nil {
		return err
	}

	logger.Info("Register mutation webhook")
	wh2 := ctrAdmission.NewMutatingAdmissionWebhook(namespace, mgr, mgr.GetClient())
	if err := wh2.Register(certDir); err != nil {
		return err
	}
	return nil
}
