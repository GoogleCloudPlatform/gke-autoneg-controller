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
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type BackendController interface {
	ReconcileBackends(AutonegStatus, AutonegStatus) error
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

	svc := &corev1.Service{}
	err := r.Get(ctx, req.NamespacedName, svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.
			return r.reconcileResult(nil)
		}
		// Error reading the object - requeue the request.
		return r.reconcileResult(err)
	}

	status, ok, err := getStatuses(ctx, svc.Namespace, svc.Name, svc.ObjectMeta.Annotations, r)
	// Is this service using autoneg?
	if !ok {
		return r.reconcileResult(nil)
	}

	if err != nil {
		r.Recorder.Event(svc, "Warning", "ConfigError", err.Error())
		return r.reconcileResult(err)
	}

	deleting := false
	// Process deletion
	if !svc.ObjectMeta.DeletionTimestamp.IsZero() && (containsString(svc.ObjectMeta.Finalizers, oldAutonegFinalizer) || containsString(svc.ObjectMeta.Finalizers, autonegFinalizer)) {
		logger.Info("Deleting service")
		deleting = true
	}

	intendedStatus := AutonegStatus{
		AutonegConfig: status.config,
		NEGStatus:     status.negStatus,
	}
	logger.Info("Existing status", "status", status)
	if status.syncConfig != nil {
		intendedStatus.AutonegSyncConfig = status.syncConfig
	}

	oldIntendedStatus := OldAutonegStatus{
		OldAutonegConfig: status.oldConfig,
		NEGStatus:        status.negStatus,
	}

	if deleting {
		intendedStatus.BackendServices = make(map[string]map[string]AutonegNEGConfig, 0)
	} else if reflect.DeepEqual(status.status, intendedStatus) && !r.AlwaysReconcile {
		// Equal, no reconciliation necessary
		return r.reconcileResult(nil)
	}

	// Reconcile differences
	logger.Info("Applying intended status", "status", intendedStatus)

	if err = r.ReconcileBackends(status.status, intendedStatus); err != nil {
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
		svc.ObjectMeta.Finalizers = removeString(svc.ObjectMeta.Finalizers, oldAutonegFinalizer)
		svc.ObjectMeta.Finalizers = removeString(svc.ObjectMeta.Finalizers, autonegFinalizer)
		delete(svc.ObjectMeta.Annotations, autonegStatusAnnotation)
		delete(svc.ObjectMeta.Annotations, oldAutonegStatusAnnotation)
	} else {
		// Remove old finalizer
		if containsString(svc.ObjectMeta.Finalizers, oldAutonegFinalizer) {
			logger.Info("Upgrading finalizer")
			svc.ObjectMeta.Finalizers = removeString(svc.ObjectMeta.Finalizers, oldAutonegFinalizer)
			svc.ObjectMeta.Finalizers = append(svc.ObjectMeta.Finalizers, autonegFinalizer)
		} else {
			// Add the finalizer annotation if it doesn't exist.
			if !containsString(svc.ObjectMeta.Finalizers, autonegFinalizer) {
				logger.Info("Adding finalizer")
				svc.ObjectMeta.Finalizers = append(svc.ObjectMeta.Finalizers, autonegFinalizer)
			}
		}

		// Write status to annotations
		anStatus, err := json.Marshal(intendedStatus)
		if err != nil {
			logger.Error(err, "json marshal error")
			return r.reconcileResult(err)
		}
		svc.ObjectMeta.Annotations[autonegStatusAnnotation] = string(anStatus)

		if !status.newConfig {
			oldStatus, err := json.Marshal(oldIntendedStatus)

			if err != nil {
				logger.Error(err, "json marshal error")
				return r.reconcileResult(err)
			}

			svc.ObjectMeta.Annotations[oldAutonegStatusAnnotation] = string(oldStatus)
		}
	}

	if err = r.Update(ctx, svc); err != nil {
		// Do not record an event in case of routine object conflict.
		if !apierrors.IsConflict(err) {
			r.Recorder.Event(svc, "Warning", "BackendError", err.Error())
		}
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
