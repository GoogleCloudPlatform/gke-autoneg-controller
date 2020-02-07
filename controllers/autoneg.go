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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/cenkalti/backoff"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

const (
	oldAutonegAnnotation          = "anthos.cft.dev/autoneg"
	autonegAnnotation             = "controller.autoneg.dev/neg"
	oldAutonegStatusAnnotation    = "anthos.cft.dev/autoneg-status"
	autonegStatusAnnotation       = "controller.autoneg.dev/neg-status"
	negStatusAnnotation           = "cloud.google.com/neg-status"
	negAnnotation                 = "cloud.google.com/neg"
	autonegFinalizer              = "anthos.cft.dev/autoneg"
	computeOperationStatusDone    = "DONE"
	computeOperationStatusRunning = "RUNNING"
	computeOperationStatusPending = "PENDING"
	maxElapsedTime                = 4 * time.Minute
)

var (
	errNotFound      = errors.New("backend service not found")
	errConfigInvalid = errors.New("autoneg configuration invalid")
	errJSONInvalid   = errors.New("json malformed")
)

// Backend returns a compute.Backend struct specified with a backend group
// and the embedded AutonegConfig
func (s AutonegStatus) Backend(port string, group string) compute.Backend {
	if s.AutonegConfig.BackendServices[port].Rate > 0 {
		return compute.Backend{
			Group:              group,
			BalancingMode:      "RATE",
			MaxRatePerEndpoint: s.AutonegConfig.BackendServices[port].Rate,
			CapacityScaler:     1,
		}
	} else {
		return compute.Backend{
			Group:                     group,
			BalancingMode:             "CONNECTION",
			MaxConnectionsPerEndpoint: int64(s.AutonegConfig.BackendServices[port].Connections),
			CapacityScaler:            1,
		}
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
	// Perform locking to ensure we patch the intended object version
	p := compute.NewBackendServicesService(b.s).Patch(b.project, name, svc)
	p.Header().Set("If-match", svc.Header.Get("ETag"))
	res, err := p.Do()
	if err != nil {
		return err
	}
	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = maxElapsedTime
	err = backoff.Retry(
		func() error {
			op, err := compute.NewGlobalOperationsService(b.s).Get(b.project, res.Name).Do()
			if err != nil {
				return err
			}
			return checkOperation(op)
		}, bo)
	return err
}

func checkOperation(op *compute.Operation) error {
	switch op.Status {
	case computeOperationStatusPending:
		return errors.New("operation pending")
	case computeOperationStatusRunning:
		return errors.New("operation running")
	case computeOperationStatusDone:
		if op.Error != nil {
			// patch operation failed
			return fmt.Errorf("operation %d failed", op.Id)
		}
		return nil
	}
	return fmt.Errorf("unknown operation state: %s", op.Status)
}

// ReconcileBackends takes the actual and intended AutonegStatus
// and attempts to apply the intended status or return an error
func (b *BackendController) ReconcileBackends(actual, intended AutonegStatus) (err error) {
	removes, upserts := ReconcileStatus(b.project, actual, intended)
	var oldSvc map[string]*compute.BackendService

	// Lookup backend services for the removals
	for port, remove := range removes {
		oldSvc[port], err = b.getBackendService(remove.name)
		if err != nil {
			return
		}
	}

	// If we are changing backend services, operate on the current backend service
	for port, upsert := range upserts {
		var newSvc *compute.BackendService
		remove := removes[port]

		if upsert.name != remove.name {
			if newSvc, err = b.getBackendService(upsert.name); err != nil {
				return
			}
		} else {
			newSvc = oldSvc[port]
		}

		// Remove backends in the list to be deleted
		for _, d := range remove.backends {
			for i, be := range oldSvc[port].Backends {
				if d.Group == be.Group {
					oldSvc[port].Backends = append(oldSvc[port].Backends[:i], oldSvc[port].Backends[i+1:]...)
					break
				}
			}
		}

		// If we are changing backend services, save the old service
		if upsert.name != remove.name {
			if err = b.updateBackends(remove.name, oldSvc[port]); err != nil {
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
					be.MaxConnectionsPerEndpoint = u.MaxConnectionsPerEndpoint
					copy = false
					break
				}
			}
			if copy {
				newBackend := u
				newSvc.Backends = append(newSvc.Backends, &newBackend)
			}
		}
		err = b.updateBackends(upsert.name, newSvc)
		if err != nil {
			return err
		}
	}

	return nil
}

// ReconcileStatus takes the actual and intended AutonegStatus
// and returns sets of backends to remove, and to upsert
func ReconcileStatus(project string, actual AutonegStatus, intended AutonegStatus) (removes, upserts map[string]Backends) {
	upserts = map[string]Backends{}
	removes = map[string]Backends{}

	// transform into maps with backend group as key
	actualBE := map[string]map[string]struct{}{}
	for port, neg := range actual.NEGs {
		actualBE[port] = map[string]struct{}{}
		for _, zone := range actual.Zones {
			group := getGroup(project, zone, neg)
			actualBE[port][group] = struct{}{}
		}
	}

	intendedBE := map[string]map[string]struct{}{}
	for port, neg := range intended.NEGs {
		intendedBE[port] = map[string]struct{}{}
		for _, zone := range intended.Zones {
			group := getGroup(project, zone, neg)
			intendedBE[port][group] = struct{}{}
		}
	}

	var intendedBEKeys []string
	for k := range intendedBE {
		intendedBEKeys = append(intendedBEKeys, k)
	}
	sort.Strings(intendedBEKeys)

	for _, port := range intendedBEKeys {
		groups := intendedBE[port]
		upsert := Backends{name: intended.BackendServices[port].Name}

		var groupsKeys []string
		for k := range groups {
			groupsKeys = append(groupsKeys, k)
		}
		sort.Strings(groupsKeys)

		for _, i := range groupsKeys {
			be := intended.Backend(port, i)
			upsert.backends = append(upsert.backends, be)
		}
		upserts[port] = upsert

		remove := Backends{name: intended.BackendServices[port].Name}
		// test to see if we are changing backend services
		if actual.BackendServices[port].Name == intended.BackendServices[port].Name || actual.BackendServices[port].Name == "" {
			// find backends to be deleted
			for a := range actualBE[port] {
				if _, ok := intendedBE[port][a]; !ok {
					remove.backends = append(remove.backends, compute.Backend{Group: a})
				}
			}
			removes[port] = remove
		} else {
			// moving to a different backend service means removing all actual backends
			remove.name = actual.BackendServices[port].Name
			for a := range actualBE[port] {
				remove.backends = append(remove.backends, compute.Backend{Group: a})
			}
			removes[port] = remove
		}
	}

	return
}

func getGroup(project, zone, neg string) string {
	return fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/networkBackendServices/%s", project, zone, neg)
}

func validateOldConfig(cfg OldAutonegConfig) error {
	// do additional validation
	return nil
}

func validateNewConfig(cfg AutonegConfig) error {
	// do additional validation
	return nil
}

func getStatuses(name string, annotations map[string]string) (s Statuses, valid bool, err error) {
	fmt.Fprintf(os.Stderr, "GET STATUS: %v and %v\n", negAnnotation, annotations)
	// Read the current cloud.google.com/neg annotation
	tmp, ok := annotations[negAnnotation]
	if ok {
		// Found a status, decode
		if err = json.Unmarshal([]byte(tmp), &s.negConfig); err != nil {
			return
		}
	}

	// Is this service using autoneg in new mode?
	oldOk := false
	tmp, newOk := annotations[autonegAnnotation]
	if newOk {
		valid = true

		if err = json.Unmarshal([]byte(tmp), &s.nanConfig); err != nil {
			return
		}

		// Default to the k8s service name + port
		for port, cfg := range s.nanConfig.BackendServices {
			if cfg.Name == "" {
				s.nanConfig.BackendServices[port] = AutonegNEGConfig{
					Name:        fmt.Sprintf("%s-%s", name, port),
					Rate:        cfg.Rate,
					Connections: cfg.Connections,
				}
			}
		}

		// Is this autoneg config valid?
		if err = validateNewConfig(s.nanConfig); err != nil {
			return
		}

		s.newConfig = true

		tmp, ok = annotations[autonegStatusAnnotation]
		if ok {
			// Found a status, decode
			if err = json.Unmarshal([]byte(tmp), &s.nanStatus); err != nil {
				return
			}
		}
	}
	if !newOk {
		// Is this service using autoneg in legacy mode?
		tmp, oldOk = annotations[oldAutonegAnnotation]
		if oldOk {
			valid = true

			if err = json.Unmarshal([]byte(tmp), &s.anConfig); err != nil {
				return
			}

			// Default to the k8s service name
			if s.anConfig.Name == "" {
				s.anConfig.Name = name
			}

			// Is this autoneg config valid?
			if err = validateOldConfig(s.anConfig); err != nil {
				return
			}

			// Convert the old configuration to a new style configuration
			s.nanConfig.BackendServices = make(map[string]AutonegNEGConfig, 1)
			if len(s.negConfig.ExposedPorts) == 1 {
				var firstPort string
				for k, _ := range s.negConfig.ExposedPorts {
					firstPort = k
					break
				}
				s.nanConfig.BackendServices[firstPort] = AutonegNEGConfig{
					Name:        s.anConfig.Name,
					Rate:        s.anConfig.Rate,
					Connections: 0,
				}
			} else {
				err = errors.New(fmt.Sprintf("more than one port in %s, but autoneg configuration is for one or no ports", negAnnotation))
				return
			}

			tmp, ok = annotations[oldAutonegStatusAnnotation]
			if ok {
				// Found a status, decode
				if err = json.Unmarshal([]byte(tmp), &s.anStatus); err != nil {
					return
				}
			}
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
