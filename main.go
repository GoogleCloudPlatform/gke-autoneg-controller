/*
Copyright 2019 Google LLC.

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

	"cloud.google.com/go/compute/metadata"
	"github.com/GoogleCloudPlatform/gke-autoneg-controller/controllers"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

const useragent = "google-pso-tool/gke-autoneg-controller/0.9.5-dev"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = corev1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var serviceNameTemplate string
	var allowServiceName bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&serviceNameTemplate, "default-backendservice-name", "{name}-{port}",
		"A naming template consists of {namespace}, {name}, {port} or {hash} separated by hyphens, "+
			"where {hash} is the first 8 digits of a hash of other given information")
	flag.BoolVar(&allowServiceName, "enable-custom-service-names", true, "Enable using custom service names in autoneg annotation.")
	flag.Parse()

	ctrl.SetLogger(zap.Logger(true))

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

	if !controllers.IsValidServiceNameTemplate(serviceNameTemplate) {
		err = fmt.Errorf("invalid service name template %s", serviceNameTemplate)
		setupLog.Error(err, "invalid service name template")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.ServiceReconciler{
		Client:              mgr.GetClient(),
		BackendController:   controllers.NewBackendController(project, s),
		Recorder:            mgr.GetEventRecorderFor("autoneg-controller"),
		Log:                 ctrl.Log.WithName("controllers").WithName("Service"),
		ServiceNameTemplate: serviceNameTemplate,
		AllowServiceName:    allowServiceName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Service")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

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
