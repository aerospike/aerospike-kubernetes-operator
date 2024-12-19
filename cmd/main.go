package main

import (
	"flag"
	"fmt"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/backup"
	backupservice "github.com/aerospike/aerospike-kubernetes-operator/internal/controller/backup-service"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	// +kubebuilder:scaffold:imports
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/restore"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/configschema"
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
	var (
		metricsAddr        string
		healthAddr         string
		webhookPort        int
		leaderResourceName string
		leaderElect        bool
	)

	flag.StringVar(&metricsAddr, "metrics-addr", "127.0.0.1:8080",
		"The address the metric endpoint binds to.")
	flag.StringVar(&healthAddr, "health-addr", ":8081",
		"The address the health endpoint binds to.")
	flag.StringVar(&leaderResourceName, "resource-name", "",
		"Name of the resource that will be used as the leader election lock.")
	flag.IntVar(&webhookPort, "webhook-port", 9443,
		"Webhook server port.")
	flag.BoolVar(&leaderElect, "leader-elect", true,
		"Enable leader election for controller manager.")

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

	// Create a new controller option for controller manager
	options := ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: healthAddr,
		LeaderElectionID:       leaderResourceName,
		LeaderElection:         leaderElect,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: webhookPort,
		}),
	}

	// Add support for multiple namespaces given in WATCH_NAMESPACE (e.g. ns1,ns2)
	if strings.Contains(watchNs, ",") {
		nsList := strings.Split(watchNs, ",")

		namespaces := make(map[string]cache.Config)

		for _, ns := range nsList {
			namespaces[strings.TrimSpace(ns)] = cache.Config{}
		}

		options.Cache.DefaultNamespaces = namespaces
	} else {
		options.Cache.DefaultNamespaces = map[string]cache.Config{
			watchNs: {},
		}
	}

	kubeConfig := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(kubeConfig, options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	kubeClient := kubernetes.NewForConfigOrDie(kubeConfig)

	// This client will read/write directly from api-server
	client, err := crClient.New(kubeConfig, crClient.Options{
		HTTPClient: mgr.GetHTTPClient(),
		Scheme:     mgr.GetScheme(),
		Mapper:     mgr.GetRESTMapper(),
	})
	if err != nil {
		setupLog.Error(err, "unable to initialize Kubernetes client")
		os.Exit(1)
	}

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

	if err = (&cluster.AerospikeClusterReconciler{
		Client:     client,
		KubeClient: kubeClient,
		KubeConfig: kubeConfig,
		Log:        ctrl.Log.WithName("controller").WithName("AerospikeCluster"),
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

	if err = (&backupservice.AerospikeBackupServiceReconciler{
		Client: client,
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controller").WithName("AerospikeBackupService"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AerospikeBackupService")
		os.Exit(1)
	}

	if err = (&asdbv1beta1.AerospikeBackupService{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "AerospikeBackupService")
		os.Exit(1)
	}

	if err = (&backup.AerospikeBackupReconciler{
		Client: client,
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controller").WithName("AerospikeBackup"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AerospikeBackup")
		os.Exit(1)
	}

	if err = (&asdbv1beta1.AerospikeBackup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "AerospikeBackup")
		os.Exit(1)
	}

	if err = (&restore.AerospikeRestoreReconciler{
		Client: client,
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controller").WithName("AerospikeRestore"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AerospikeRestore")
		os.Exit(1)
	}

	if err = (&asdbv1beta1.AerospikeRestore{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "AerospikeRestore")
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
