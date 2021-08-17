/*
Copyright 2019-2021 Google LLC.

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

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	*BackendController
	Recorder            record.EventRecorder
	Log                 logr.Logger
	ServiceNameTemplate string
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *ServiceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("service", req.NamespacedName)

	svc := &corev1.Service{}
	err := r.Get(ctx, req.NamespacedName, svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	status, ok, err := getStatuses(svc.Namespace, svc.Name, svc.ObjectMeta.Annotations, r.ServiceNameTemplate)
	// Is this service using autoneg?
	if !ok {
		return reconcile.Result{}, nil
	}

	if err != nil {
		r.Recorder.Event(svc, "Warning", "ConfigError", err.Error())
		return reconcile.Result{}, err
	}

	deleting := false
	// Process deletion
	if !svc.ObjectMeta.DeletionTimestamp.IsZero() && containsString(svc.ObjectMeta.Finalizers, autonegFinalizer) {
		logger.Info("Deleting service")
		deleting = true
	}

	intendedStatus := AutonegStatus{
		AutonegConfig: status.config,
		NEGStatus:     status.negStatus,
	}

	oldIntendedStatus := OldAutonegStatus{
		OldAutonegConfig: status.oldConfig,
		NEGStatus:        status.negStatus,
	}

	if deleting {
		intendedStatus.NEGStatus = NEGStatus{}
	}

	if reflect.DeepEqual(status.status, intendedStatus) {
		// Equal, no reconciliation necessary
		return reconcile.Result{}, nil
	}

	// Reconcile differences
	logger.Info("Applying intended status", "status", intendedStatus)

	if err = r.ReconcileBackends(status.status, intendedStatus); err != nil {
		var e *errNotFound
		if !(deleting && errors.As(err, &e)) {
			r.Recorder.Event(svc, "Warning", "BackendError", err.Error())
			return reconcile.Result{}, err
		}
	}

	// Write changes to the service object.
	if deleting {
		// Remove finalizer and clear status
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
			return reconcile.Result{}, err
		}
		svc.ObjectMeta.Annotations[autonegStatusAnnotation] = string(anStatus)

		if !status.newConfig {
			oldStatus, err := json.Marshal(oldIntendedStatus)

			if err != nil {
				logger.Error(err, "json marshal error")
				return reconcile.Result{}, err
			}

			svc.ObjectMeta.Annotations[oldAutonegStatusAnnotation] = string(oldStatus)
		}
	}

	if err = r.Update(ctx, svc); err != nil {
		// Do not record an event in case of routine object conflict.
		if !apierrors.IsConflict(err) {
			r.Recorder.Event(svc, "Warning", "BackendError", err.Error())
		}
		return reconcile.Result{}, err
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

	return reconcile.Result{}, nil
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}

//
// Helper functions to check and remove string from a slice of strings.
//
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
