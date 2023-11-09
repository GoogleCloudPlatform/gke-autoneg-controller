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
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"cloud.google.com/go/compute/metadata"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/GoogleCloudPlatform/gke-autoneg-controller/controllers"
	//+kubebuilder:scaffold:imports
)

const useragent = "google-pso-tool/gke-autoneg-controller/1.0.0"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
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
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.Float64Var(&maxRatePerEndpointDefault, "max-rate-per-endpoint", 0, "Default max rate per endpoint. Can be overridden by user config.")
	flag.Float64Var(&maxConnectionsPerEndpointDefault, "max-connections-per-endpoint", 0, "Default max connections per endpoint. Can be overridden by user config.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&serviceNameTemplate, "default-backendservice-name", "{name}-{port}",
		"A naming template consists of {namespace}, {name}, {port} or {hash} separated by hyphens, "+
			"where {hash} is the first 8 digits of a hash of other given information")
	flag.BoolVar(&allowServiceName, "enable-custom-service-names", true, "Enable using custom service names in autoneg annotation.")
	flag.BoolVar(&alwaysReconcile, "always-reconcile", false, "Periodically reconciles even if annotation statuses don't change.")
	flag.StringVar(&reconcilePeriod, "reconcile-period", "", "The minimum frequency at which watched resources are reconciled, e.g. 10m. Defaults to 10h if not set.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := compute.NewService(ctx, option.WithUserAgent(useragent))
	if err != nil {
		setupLog.Error(err, "can't request Google compute service")
		os.Exit(1)
	}

	project := getProject()
	if project == "" {
		setupLog.Error(err, "can't determine project ID")
		os.Exit(1)
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9fe89c94.controller.autoneg.dev",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.ServiceReconciler{
		Client:                           mgr.GetClient(),
		Scheme:                           mgr.GetScheme(),
		BackendController:                controllers.NewBackendController(project, s),
		Recorder:                         mgr.GetEventRecorderFor("autoneg-controller"),
		ServiceNameTemplate:              serviceNameTemplate,
		AllowServiceName:                 allowServiceName,
		MaxRatePerEndpointDefault:        maxRatePerEndpointDefault,
		MaxConnectionsPerEndpointDefault: maxConnectionsPerEndpointDefault,
		AlwaysReconcile:                  alwaysReconcile,
		ReconcileDuration:                &reconcileDuration,
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
