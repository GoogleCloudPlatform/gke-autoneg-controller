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

package controllers

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

const (
	autonegAnnotation       = "anthos.cft.dev/autoneg"
	autonegStatusAnnotation = "anthos.cft.dev/autoneg-status"
	negStatusAnnotation     = "cloud.google.com/neg-status"
	autonegFinalizer        = "anthos.cft.dev/autoneg"
)

var (
	errNotFound      = errors.New("backend service not found")
	errConfigInvalid = errors.New("autoneg configuration invalid")
	errJSONInvalid   = errors.New("json malformed")
)

// Backend returns a compute.Backend struct specified with a backend group
// and the embedded AutonegConfig
func (s AutonegStatus) Backend(group string) compute.Backend {
	return compute.Backend{
		Group:              group,
		BalancingMode:      "RATE",
		MaxRatePerEndpoint: s.AutonegConfig.Rate,
		CapacityScaler:     1,
	}
}

// NewBackendController takes the project name and an initialized *compute.Service
func NewBackendController(project string, s *compute.Service) *BackendController {
	return &BackendController{
		project: project,
		s:       s,
	}
}

func (b *BackendController) getBackendService(name string) (svc *compute.BackendService, err error) {
	svc, err = compute.NewBackendServicesService(b.s).Get(b.project, name).Do()
	if e, ok := err.(*googleapi.Error); ok {
		if e.Code == 404 {
			err = errNotFound
		}
	}
	return
}

func (b *BackendController) updateBackends(name string, svc *compute.BackendService) error {
	if len(svc.Backends) == 0 {
		svc.NullFields = []string{"Backends"}
	}
	// Perform optimistic locking to ensure we patch the intended object version
	p := compute.NewBackendServicesService(b.s).Patch(b.project, name, svc)
	p.Header().Set("If-match", svc.Header.Get("ETag"))
	_, err := p.Do()
	return err
}

// ReconcileBackends takes the actual and intended AutonegStatus
// and attempts to apply the intended status or return an error
func (b *BackendController) ReconcileBackends(actual, intended AutonegStatus) (err error) {
	remove, upsert := ReconcileStatus(b.project, actual, intended)

	var oldSvc, newSvc *compute.BackendService
	oldSvc, err = b.getBackendService(remove.name)
	if err != nil {
		return
	}

	// If we are changing backend services, operate on the current backend service
	if upsert.name != remove.name {
		if newSvc, err = b.getBackendService(upsert.name); err != nil {
			return
		}
	} else {
		newSvc = oldSvc
	}

	// Remove backends in the list to be deleted
	for _, d := range remove.backends {
		for i, be := range oldSvc.Backends {
			if d.Group == be.Group {
				oldSvc.Backends = append(oldSvc.Backends[:i], oldSvc.Backends[i+1:]...)
				break
			}
		}
	}

	// If we are changing backend services, save the old service
	if upsert.name != remove.name {
		if err = b.updateBackends(remove.name, oldSvc); err != nil {
			return
		}
	}

	// Add or update any new backends to the list
	for _, u := range upsert.backends {
		copy := true
		for _, be := range newSvc.Backends {
			if u.Group == be.Group {
				// TODO: copy fields explicitly
				be.MaxRatePerEndpoint = u.MaxRatePerEndpoint
				copy = false
				break
			}
		}
		if copy {
			newBackend := u
			newSvc.Backends = append(newSvc.Backends, &newBackend)
		}
	}

	return b.updateBackends(upsert.name, newSvc)
}

// ReconcileStatus takes the actual and intended AutonegStatus
// and returns sets of backends to remove, and to upsert
func ReconcileStatus(project string, actual AutonegStatus, intended AutonegStatus) (remove, upsert Backends) {
	// transform into maps with backend group as key
	actualBE := map[string]struct{}{}
	for _, neg := range actual.NEGs {
		for _, zone := range actual.Zones {
			group := getGroup(project, zone, neg)
			actualBE[group] = struct{}{}
		}
	}

	intendedBE := map[string]struct{}{}
	for _, neg := range intended.NEGs {
		for _, zone := range intended.Zones {
			group := getGroup(project, zone, neg)
			intendedBE[group] = struct{}{}
		}
	}

	// all intended backends are to be upserted
	upsert.name = intended.Name
	for i := range intendedBE {
		be := intended.Backend(i)
		upsert.backends = append(upsert.backends, be)
	}

	// test to see if we are changing backend services
	if actual.Name == intended.Name || actual.Name == "" {
		// find backends to be deleted
		remove.name = intended.Name
		for a := range actualBE {
			if _, ok := intendedBE[a]; !ok {
				remove.backends = append(remove.backends, compute.Backend{Group: a})
			}
		}
	} else {
		// moving to a different backend service means removing all actual backends
		remove.name = actual.Name
		for a := range actualBE {
			remove.backends = append(remove.backends, compute.Backend{Group: a})
		}
	}

	return
}

func getGroup(project, zone, neg string) string {
	return fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/networkEndpointGroups/%s", project, zone, neg)
}

func validateConfig(cfg AutonegConfig) error {
	// do additional validation
	return nil
}

func getStatuses(name string, annotations map[string]string) (s Statuses, valid bool, err error) {
	// Is this service using autoneg?
	tmp, ok := annotations[autonegAnnotation]
	if !ok {
		return
	}
	valid = true

	if err = json.Unmarshal([]byte(tmp), &s.anConfig); err != nil {
		return
	}

	// Default to the k8s service name
	if s.anConfig.Name == "" {
		s.anConfig.Name = name
	}

	// Is this autoneg config valid?
	if err = validateConfig(s.anConfig); err != nil {
		return
	}

	tmp, ok = annotations[autonegStatusAnnotation]
	if ok {
		// Found a status, decode
		if err = json.Unmarshal([]byte(tmp), &s.anStatus); err != nil {
			return
		}
	}

	tmp, ok = annotations[negStatusAnnotation]
	if ok {
		// Found a status, decode
		if err = json.Unmarshal([]byte(tmp), &s.negStatus); err != nil {
			return
		}
	}

	return
}
