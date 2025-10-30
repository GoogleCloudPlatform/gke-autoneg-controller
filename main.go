/*
Copyright 2021 Google LLC.

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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"cloud.google.com/go/compute/metadata"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/GoogleCloudPlatform/gke-autoneg-controller/controllers"
	//+kubebuilder:scaffold:imports
)

const useragent = "google-pso-tool/gke-autoneg-controller/2.0.0"

var (
	scheme    = runtime.NewScheme()
	setupLog  = ctrl.Log.WithName("setup")
	BuildTime string
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var maxRatePerEndpointDefault float64
	var maxConnectionsPerEndpointDefault float64
	var enableLeaderElection bool
	var serviceNameTemplate string
	var allowServiceName bool
	var alwaysReconcile bool
	var reconcilePeriod string
	var namespaces string
	var project string
	var useSvcNeg bool
	var deregisterNEGsOnAnnotationRemoval bool
	var debug bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.Float64Var(&maxRatePerEndpointDefault, "max-rate-per-endpoint", 0, "Default max rate per endpoint. Can be overridden by user config.")
	flag.Float64Var(&maxConnectionsPerEndpointDefault, "max-connections-per-endpoint", 0, "Default max connections per endpoint. Can be overridden by user config.")
	flag.StringVar(&namespaces, "namespaces", "", "List of namespaces where Services should be reconciled.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&serviceNameTemplate, "default-backendservice-name", "{name}-{port}",
		"A naming template consists of {namespace}, {name}, {port} or {hash} separated by hyphens, "+
			"where {hash} is the first 8 digits of a hash of other given information")
	flag.BoolVar(&allowServiceName, "enable-custom-service-names", true, "Enable using custom service names in autoneg annotation.")
	flag.BoolVar(&alwaysReconcile, "always-reconcile", true, "Periodically reconciles even if annotation statuses don't change.")
	flag.StringVar(&reconcilePeriod, "reconcile-period", "5m", "The minimum frequency at which watched resources are reconciled, e.g. 10m. Defaults to 5m if not set.")
	flag.BoolVar(&deregisterNEGsOnAnnotationRemoval, "deregister-negs-on-annotation-removal", true, "Deregister NEGs from backend service when annotation removed.")
	flag.StringVar(&project, "project-id", "", "The project ID of the Google Cloud project where the backend services are created. If not specified, project ID will be fetched from the Metadata server.")
	flag.BoolVar(&useSvcNeg, "use-svcneg", true, "Use service neg custom resource to get the NEG zone info.")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging.")

	opts := zap.Options{
		Development: debug,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Set logger globally before creating manager to ensure leader election uses same format
	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	// Configure klog to use our zap logger for client-go components (like leader election)
	klog.SetLogger(logger)

	if useSvcNeg {
		utilruntime.Must(v1beta1.AddToScheme(scheme))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := compute.NewService(ctx, option.WithUserAgent(useragent))
	if err != nil {
		setupLog.Error(err, "can't request Google compute service")
		os.Exit(1)
	}

	// Use the user-provided project-ID if specified.
	if project == "" {
		project = getProject()
		if project == "" {
			setupLog.Error(err, "can't determine project ID")
			os.Exit(1)
		}
	}

	var reconcileDuration time.Duration
	if reconcilePeriod != "" {
		reconcileDuration, err = time.ParseDuration(reconcilePeriod)
		if err != nil {
			setupLog.Error(err, "Invalid reconcilePeriod")
			os.Exit(1)
		}
	}

	if !controllers.IsValidServiceNameTemplate(serviceNameTemplate) {
		err = fmt.Errorf("invalid service name template %s", serviceNameTemplate)
		setupLog.Error(err, "invalid service name template")
		os.Exit(1)
	}

	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2 for metrics server")
		c.NextProtos = []string{"http/1.1"}
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:    metricsAddr,
		SecureServing:  true,
		TLSOpts:        []func(*tls.Config){disableHTTP2},
		FilterProvider: filters.WithAuthenticationAndAuthorization,
	}

	mgrOpts := ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsServerOptions,
		// Port:                   9443, // Webhook server will default to port 9443
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9fe89c94.controller.autoneg.dev",
		Logger:                 logger, // Ensure manager uses the same logger for leader election
		NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
			if namespaces != "" {
				opts.DefaultNamespaces = make(map[string]cache.Config, 0)
				for _, ns := range strings.Split(namespaces, ",") {
					opts.DefaultNamespaces[ns] = cache.Config{}
				}
			}
			return cache.New(config, opts)
		},
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.ServiceReconciler{
		Client:                            mgr.GetClient(),
		Scheme:                            mgr.GetScheme(),
		BackendController:                 controllers.NewBackendController(project, s),
		Recorder:                          mgr.GetEventRecorderFor("autoneg-controller"),
		ServiceNameTemplate:               serviceNameTemplate,
		AllowServiceName:                  allowServiceName,
		MaxRatePerEndpointDefault:         maxRatePerEndpointDefault,
		MaxConnectionsPerEndpointDefault:  maxConnectionsPerEndpointDefault,
		AlwaysReconcile:                   alwaysReconcile,
		DeregisterNEGsOnAnnotationRemoval: deregisterNEGsOnAnnotationRemoval,
		ReconcileDuration:                 &reconcileDuration,
		UseSvcNeg:                         useSvcNeg,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Service")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info(fmt.Sprintf("Build time: %s", BuildTime))
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getProject() string {
	// probe metadata service for project, or fall back to PROJECT_ID in environment
	p, err := metadata.ProjectID()
	if err == nil {
		return p
	}
	return os.Getenv("PROJECT_ID")
}
