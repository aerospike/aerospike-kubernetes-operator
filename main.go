package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	// +kubebuilder:scaffold:imports
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/configschema"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

var (
	scheme   = k8Runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	// +kubebuilder:scaffold:scheme
	utilRuntime.Must(asdbv1.AddToScheme(scheme))
	utilRuntime.Must(clientGoScheme.AddToScheme(scheme))
	utilRuntime.Must(asdbv1beta1.AddToScheme(scheme))
}

func main() {
	var configFile string

	flag.StringVar(&configFile, "config", "controller_manager_config.yaml",
		"The controller will load its initial configuration from this file. "+
			"Omit this flag to use the default configuration values. "+
			"Command-line flags override configuration from this file.",
	)

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	watchNs, err := getWatchNamespace()
	if err != nil {
		setupLog.Error(err, "Failed to get watch namespace")
		os.Exit(1)
	}

	var webhookServer *webhook.Server

	legacyOlmCertDir := "/apiserver.local.config/certificates"
	// If legacy directory is present then OLM < 0.17 is used and webhook server should be configured as follows
	info, err := os.Stat(legacyOlmCertDir)
	if err == nil && info.IsDir() {
		setupLog.Info(
			"legacy OLM < 0.17 directory is present - initializing webhook" +
				" server ",
		)

		webhookServer = &webhook.Server{
			CertDir:  "/apiserver.local.config/certificates",
			CertName: "apiserver.crt",
			KeyName:  "apiserver.key",
		}
	}

	// Create a new controller option for controller manager
	options := ctrl.Options{
		NewClient: newClient,
		Scheme:    scheme,
		// if webhookServer is nil, which will be the case of OLM >= 0.17,
		// the manager will create a server for you using Host, Port
		// and the default CertDir, KeyName, and CertName.
		WebhookServer: webhookServer,
	}

	options, err = options.AndFrom(ctrl.ConfigFile().AtPath(configFile))
	if err != nil {
		setupLog.Error(err, "unable to load the config file")
		os.Exit(1)
	}

	// Add support for multiple namespaces given in WATCH_NAMESPACE (e.g. ns1,ns2)
	// For more Info: https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/cache#MultiNamespacedCacheBuilder
	if strings.Contains(watchNs, ",") {
		nsList := strings.Split(watchNs, ",")

		var newNsList []string
		for _, ns := range nsList {
			newNsList = append(newNsList, strings.TrimSpace(ns))
		}

		options.NewCache = cache.MultiNamespacedCacheBuilder(newNsList)
	} else {
		options.Namespace = watchNs
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	kubeConfig := ctrl.GetConfigOrDie()
	kubeClient := kubernetes.NewForConfigOrDie(kubeConfig)

	setupLog.Info("Init aerospike-server config schemas")

	schemaMap, err := configschema.NewSchemaMap()
	if err != nil {
		setupLog.Error(err, "Unable to Load SchemaMap")
		os.Exit(1)
	}

	schemaMapLogger := ctrl.Log.WithName("schema-map")
	asconfig.InitFromMap(schemaMapLogger, schemaMap)

	eventBroadcaster := record.NewBroadcasterWithCorrelatorOptions(
		record.CorrelatorOptions{
			BurstSize: getEventBurstSize(),
			QPS:       1,
		},
	)
	// Start events processing pipeline.
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	if err = (&aerospikecluster.AerospikeClusterReconciler{
		Client:     mgr.GetClient(),
		KubeClient: kubeClient,
		KubeConfig: kubeConfig,
		Log:        ctrl.Log.WithName("controllers").WithName("AerospikeCluster"),
		Scheme:     mgr.GetScheme(),
		Recorder: eventBroadcaster.NewRecorder(
			mgr.GetScheme(), v1.EventSource{Component: "aerospikeCluster-controller"},
		),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(
			err, "unable to create controller", "controller",
			"AerospikeCluster",
		)
		os.Exit(1)
	}

	if err = (&asdbv1.AerospikeCluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "v1-webhook", "AerospikeCluster")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	eventBroadcaster.Shutdown()
}

// getWatchNamespace returns the Namespace the operator should be watching for changes
func getWatchNamespace() (string, error) {
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which specifies the Namespace to watch.
	// An empty value means the operator is running with cluster scope.
	var watchNamespaceEnvVar = "WATCH_NAMESPACE"

	ns, found := os.LookupEnv(watchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", watchNamespaceEnvVar)
	}

	return ns, nil
}

// getEventBurstSize returns the burst size of events that can be handled in the cluster
func getEventBurstSize() int {
	// EventBurstSizeEnvVar is the constant for env variable EVENT_BURST_SIZE
	// An empty value means the default burst size which is 150.
	var (
		eventBurstSizeEnvVar = "EVENT_BURST_SIZE"
		eventBurstSize       = 150
	)

	burstSize, found := os.LookupEnv(eventBurstSizeEnvVar)
	if found {
		eventBurstSizeInt, err := strconv.Atoi(burstSize)
		if err != nil {
			setupLog.Info("Invalid EVENT_BURST_SIZE value: using default 150")
		} else {
			eventBurstSize = eventBurstSizeInt
		}
	}

	return eventBurstSize
}

// newClient creates the default caching client
// this will read/write directly from api-server
func newClient(
	_ cache.Cache, config *rest.Config, options crClient.Options,
	_ ...crClient.Object,
) (crClient.Client, error) {
	return crClient.New(config, options)
}
