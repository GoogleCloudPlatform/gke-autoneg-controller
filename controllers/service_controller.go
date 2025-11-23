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

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
)

type BackendController interface {
	ReconcileBackends(context.Context, AutonegStatus, AutonegStatus, bool) error
}

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	BackendController
	Recorder                          record.EventRecorder
	ServiceNameTemplate               string
	AllowServiceName                  bool
	MaxRatePerEndpointDefault         float64
	MaxConnectionsPerEndpointDefault  float64
	AlwaysReconcile                   bool
	ReconcileDuration                 *time.Duration
	DeregisterNEGsOnAnnotationRemoval bool
	UseSvcNeg                         bool

	MetricBackendServicesPerService *prometheus.GaugeVec
	MetricNEGsPerService            *prometheus.GaugeVec
}

//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Service object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("service", req.NamespacedName)

	// Debug level logging for detailed reconciliation info
	logger.V(1).Info("Starting reconciliation for service", "namespace", req.Namespace, "name", req.Name)

	svc := &corev1.Service{}
	logger.V(1).Info("Checking Kubernetes service", "namespace", req.Namespace, "name", req.Name)
	err := r.Get(ctx, req.NamespacedName, svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.
			logger.V(1).Info("Service not found, skipping reconciliation")
			return r.reconcileResult(nil)
		}
		// Error reading thkube object - requeue the request.
		logger.Error(err, "Failed to get Kubernetes service")
		return r.reconcileResult(err)
	}
	logger.V(1).Info("Successfully retrieved Kubernetes service", "serviceType", svc.Spec.Type, "ports", len(svc.Spec.Ports))

	status, ok, err := getStatuses(ctx, svc.Namespace, svc.Name, svc.ObjectMeta.Annotations, r)
	// Is this service using autoneg?
	if !ok {
		logger.V(1).Info("Service is not using autoneg, skipping")
		return r.reconcileResult(nil)
	}
	if err != nil {
		logger.Error(err, "Configuration error for service")
		r.Recorder.Event(svc, "Warning", "ConfigError", err.Error())
		return r.reconcileResult(err)
	}

	deleting := false
	// Process deletion
	if !svc.ObjectMeta.DeletionTimestamp.IsZero() && (containsString(svc.ObjectMeta.Finalizers, autonegFinalizer)) {
		logger.Info("Deleting service")
		deleting = true
	}

	intendedStatus := AutonegStatus{
		AutonegConfig: status.config,
		NEGStatus:     status.negStatus,
	}
	logger.Info("Existing status", "status", fmt.Sprintf("%+v", status))
	if status.syncConfig != nil {
		intendedStatus.AutonegSyncConfig = status.syncConfig
	}
	if err = r.RecordMetrics(logger, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, status); err != nil {
		logger.Error(err, "Error recording metrics")
	}

	if deleting {
		intendedStatus.BackendServices = make(map[string]map[string]AutonegNEGConfig, 0)
	} else if reflect.DeepEqual(status.status, intendedStatus) && !r.AlwaysReconcile {
		// Equal, no reconciliation necessary
		return r.reconcileResult(nil)
	}

	// Reconcile differences
	logger.Info("Applying intended status", "status", intendedStatus)

	if err = r.ReconcileBackends(ctx, status.status, intendedStatus, deleting); err != nil {
		var e *errNotFound
		if !(deleting && errors.As(err, &e)) {
			r.Recorder.Event(svc, "Warning", "BackendError", err.Error())
			return r.reconcileResult(err)
		}
		if deleting {
			r.Recorder.Event(svc, "Warning", "BackendError while deleting", err.Error())
			return r.reconcileResult(err)
		}
	}

	// Write changes to the service object.
	if deleting {
		// Remove finalizer and clear status
		logger.Info("Removing finalizer")
		svc.ObjectMeta.Finalizers = removeString(svc.ObjectMeta.Finalizers, autonegFinalizer)
		delete(svc.ObjectMeta.Annotations, autonegStatusAnnotation)
	} else {
		// Add the finalizer annotation if it doesn't exist.
		if !containsString(svc.ObjectMeta.Finalizers, autonegFinalizer) {
			logger.Info("Adding finalizer")
			svc.ObjectMeta.Finalizers = append(svc.ObjectMeta.Finalizers, autonegFinalizer)
		}

		// Write status to annotations
		anStatus, err := json.Marshal(intendedStatus)
		if err != nil {
			logger.Error(err, "json marshal error")
			return r.reconcileResult(err)
		}
		svc.ObjectMeta.Annotations[autonegStatusAnnotation] = string(anStatus)
	}

	if err = r.Update(ctx, svc); err != nil {
		if apierrors.IsConflict(err) {
			// Treat object update races as transient and retry with the latest version.
			logger.Info("Conflict updating service; requeueing", "error", err.Error())
			return reconcile.Result{RequeueAfter: 1 * time.Second}, nil
		}
		r.Recorder.Event(svc, "Warning", "BackendError", err.Error())
		return r.reconcileResult(err)
	}

	for port, endpointGroups := range intendedStatus.BackendServices {
		for _, endpointGroup := range endpointGroups {
			if deleting {
				r.Recorder.Eventf(svc, "Normal", "Delete",
					"Deregistered NEGs for %q from backend service %q (port %s)",
					req.NamespacedName,
					endpointGroup.Name,
					port)

			} else {
				r.Recorder.Eventf(svc, "Normal", "Sync",
					"Synced NEGs for %q as backends to backend service %q (port %s)",
					req.NamespacedName,
					endpointGroup.Name,
					port)
			}
		}
	}

	return r.reconcileResult(nil)
}

func (r *ServiceReconciler) RegisterMetrics() {
	r.MetricBackendServicesPerService = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "backend_services_per_service",
			Help: "Number of backends services per service",
		},
		[]string{"namespace", "service", "port"},
	)

	r.MetricNEGsPerService = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "network_endpoint_groups_per_service",
			Help: "Number of NEGs per service",
		},
		[]string{"namespace", "service"},
	)
	metrics.Registry.MustRegister(r.MetricBackendServicesPerService, r.MetricNEGsPerService)
}

func (r *ServiceReconciler) RecordMetrics(logger logr.Logger, namespace string, service string, status Statuses) error {
	// Add metrics if they are set up
	if r.MetricBackendServicesPerService != nil {
		for mk, mv := range status.status.BackendServices {
			metricLabels := prometheus.Labels{
				"namespace": namespace,
				"service":   service,
				"port":      mk,
			}
			(*r.MetricBackendServicesPerService).With(metricLabels).Set(float64(len(mv)))
		}
	}
	if r.MetricNEGsPerService != nil {
		metricLabels := prometheus.Labels{
			"namespace": namespace,
			"service":   service,
		}
		(*r.MetricNEGsPerService).With(metricLabels).Set(float64(len(status.negStatus.NEGs)))
	}
	if r.MetricBackendServicesPerService != nil || r.MetricNEGsPerService != nil {
		return nil
	}
	return errors.New("no metrics configured")
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.UseSvcNeg {
		return ctrl.NewControllerManagedBy(mgr).
			For(&corev1.Service{}).
			Owns(&v1beta1.ServiceNetworkEndpointGroup{}).
			Complete(r)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func (r *ServiceReconciler) reconcileResult(err error) (reconcile.Result, error) {
	if r.ReconcileDuration != nil && r.AlwaysReconcile {
		return reconcile.Result{RequeueAfter: *r.ReconcileDuration}, err
	}
	return reconcile.Result{}, err
}
