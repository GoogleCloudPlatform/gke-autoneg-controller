/*
Copyright 2019-2023 Google LLC.

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
	"regexp"
	"slices"
	"sort"
	"time"

	backoff "github.com/cenkalti/backoff/v5"
	"github.com/go-logr/logr"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	oldAutonegAnnotation          = "anthos.cft.dev/autoneg"
	autonegAnnotation             = "controller.autoneg.dev/neg"
	oldAutonegStatusAnnotation    = "anthos.cft.dev/autoneg-status"
	autonegStatusAnnotation       = "controller.autoneg.dev/neg-status"
	negStatusAnnotation           = "cloud.google.com/neg-status"
	negAnnotation                 = "cloud.google.com/neg"
	oldAutonegFinalizer           = "anthos.cft.dev/autoneg"
	autonegFinalizer              = "controller.autoneg.dev/neg"
	autonegSyncAnnotation         = "controller.autoneg.dev/sync"
	computeOperationStatusDone    = "DONE"
	computeOperationStatusRunning = "RUNNING"
	computeOperationStatusPending = "PENDING"
	maxElapsedTime                = 4 * time.Minute
)

var (
	errConfigInvalid = errors.New("autoneg configuration invalid")
	errJSONInvalid   = errors.New("json malformed")
	zoneRE           = regexp.MustCompile(`zones/([^/]+)`)
)

type errNotFound struct {
	Name string
}

func (e *errNotFound) Error() string {
	return fmt.Sprintf("backend service not found")
}

// Backend returns a compute.Backend struct specified with a backend group
// and the embedded AutonegConfig
func (s AutonegStatus) Backend(name string, port string, group string) compute.Backend {
	cfg := s.AutonegConfig.BackendServices[port][name]

	// Extract initial_capacity setting, if set
	var capacityScaler float64 = 1
	if capacity := cfg.InitialCapacity; capacity != nil {
		// This case should not be possible since validateNewConfig checks
		// it, but leave the default setting of 100% if capacity is less
		// than 0 or greater than 100
		if *capacity >= int32(0) && *capacity <= int32(100) {
			capacityScaler = float64(*capacity) / 100
		}
	}
	if capacity := cfg.CapacityScaler; capacity != nil {
		// This case should not be possible since validateNewConfig checks
		// it, but leave the default setting of 100% if capacity is less
		// than 0 or greater than 100
		if *capacity >= int32(0) && *capacity <= int32(100) {
			capacityScaler = float64(*capacity) / 100
		}
	}

	// Prefer the rate balancing mode if set
	if cfg.Rate > 0 {
		return compute.Backend{
			Group:              group,
			BalancingMode:      "RATE",
			MaxRatePerEndpoint: cfg.Rate,
			CapacityScaler:     capacityScaler,
		}
	} else {
		return compute.Backend{
			Group:                     group,
			BalancingMode:             "CONNECTION",
			MaxConnectionsPerEndpoint: int64(cfg.Connections),
			CapacityScaler:            capacityScaler,
		}
	}
}

// NewBackendController takes the project name and an initialized *compute.Service
func NewBackendController(project string, s *compute.Service) *ProdBackendController {
	return &ProdBackendController{
		project: project,
		s:       s,
	}
}

func (b *ProdBackendController) getBackendService(name string, region string) (svc *compute.BackendService, err error) {
	if region == "" {
		svc, err = compute.NewBackendServicesService(b.s).Get(b.project, name).Do()
		if e, ok := err.(*googleapi.Error); ok {
			if e.Code == 404 {
				err = &errNotFound{Name: name}
			}
		}
	} else {
		svc, err = compute.NewRegionBackendServicesService(b.s).Get(b.project, region, name).Do()
		if e, ok := err.(*googleapi.Error); ok {
			if e.Code == 404 {
				err = &errNotFound{Name: name}
			}
		}
	}
	return

}

func (b *ProdBackendController) updateBackends(name string, region string, svc *compute.BackendService, forceCapacity map[int]bool, deleting bool) error {
	if len(svc.Backends) == 0 {
		if deleting {
			svc.ForceSendFields = []string{"Backends"}
		} else {
			svc.NullFields = []string{"Backends"}
		}
	} else {
		for beidx, be := range svc.Backends {
			if fc, ok := forceCapacity[beidx]; ok && fc {
				be.ForceSendFields = []string{"CapacityScaler"}
			}
		}
	}
	// Perform locking to ensure we patch the intended object version
	if region == "" {
		p := compute.NewBackendServicesService(b.s).Patch(b.project, name, svc)
		p.Header().Set("If-match", svc.Header.Get("ETag"))
		res, err := p.Do()
		if err != nil {
			return err
		}
		operation := func() (bool, error) {
			op, err := compute.NewGlobalOperationsService(b.s).Get(b.project, res.Name).Do()
			if err != nil {
				return false, err
			}
			return true, checkOperation(op)
		}
		_, err = backoff.Retry(context.TODO(),
			operation, backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxElapsedTime(maxElapsedTime))
		return err
	} else {
		p := compute.NewRegionBackendServicesService(b.s).Patch(b.project, region, name, svc)
		p.Header().Set("If-match", svc.Header.Get("ETag"))
		res, err := p.Do()
		if err != nil {
			return err
		}
		operation := func() (bool, error) {
			op, err := compute.NewRegionOperationsService(b.s).Get(b.project, region, res.Name).Do()
			if err != nil {
				return false, err
			}
			return true, checkOperation(op)
		}
		_, err = backoff.Retry(context.TODO(), operation,
			backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxElapsedTime(maxElapsedTime))
		return err
	}
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
func (b *ProdBackendController) ReconcileBackends(ctx context.Context, actual, intended AutonegStatus, deleting bool) (err error) {
	logger := log.FromContext(ctx)
	// Determine which backends to remove and which to insert/update.
	removes, upserts := ReconcileStatus(logger, b.project, actual, intended)
	// logger.Info("Reconciling backends", "removes", fmt.Sprintf("%+v", removes), "upserts", fmt.Sprintf("%+v", upserts))

	var forceCapacity map[int]bool = make(map[int]bool, 0)
	// Iterate over each port that has backends to be removed.
	for port, _removes := range removes {
		// Iterate over each backend service to be removed.
		for idx, remove := range _removes {
			var oldSvc *compute.BackendService
			var svcUpdated = false
			// Get the current state of the backend service.
			oldSvc, err = b.getBackendService(remove.name, remove.region)
			var e *errNotFound
			if errors.As(err, &e) {
				// If the backend service is gone, we construct a BackendService with the same name
				// and an empty list of backends.
				err = nil
				oldSvc = &compute.BackendService{
					Name:     remove.name,
					Backends: make([]*compute.Backend, 0),
				}
			} else if err != nil {
				return
			}

			var newSvc *compute.BackendService
			upsert := upserts[port][idx]

			// Check if the same port is in the upsert map and if upsert needs to happen on a different backend service.
			if upsert.name != "" && upsert.name != remove.name {
				if newSvc, err = b.getBackendService(upsert.name, upsert.region); err != nil {
					return
				}
			} else {
				newSvc = oldSvc
			}

			// Remove backends that are in the list to be deleted for this port.
			for _, d := range remove.backends {
				// Remove only the requested backends and keep the rest.
				for i, be := range oldSvc.Backends {
					if d.Group == be.Group {
						svcUpdated = true
						copy(oldSvc.Backends[i:], oldSvc.Backends[i+1:])
						oldSvc.Backends = oldSvc.Backends[:len(oldSvc.Backends)-1]
						break
					}
				}
			}

			// If a different service needs to be updated based on the upsert map entry for this port,
			// then save the existing backend service and update the new service.
			if svcUpdated && (deleting || upsert.name == "" || upsert.name != remove.name) {
				if err = b.updateBackends(remove.name, remove.region, oldSvc, forceCapacity, deleting); err != nil {
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
						if intended.AutonegSyncConfig != nil {
							var syncConfig AutonegSyncConfig = *intended.AutonegSyncConfig
							if syncConfig.CapacityScaler != nil && *syncConfig.CapacityScaler {
								be.CapacityScaler = u.CapacityScaler
							}
						} else {
							// Force CapacityScaler to an "empty value"
							u.CapacityScaler = 0
						}
						copy = false
						break
					}
				}
				if copy {
					// It's a new backend to be added
					newBackend := u
					if _, ok := intended.AutonegConfig.BackendServices[port][idx]; ok {
						if intended.AutonegConfig.BackendServices[port][idx].InitialCapacity != nil {
							forceCapacity[len(newSvc.Backends)] = true
						}
					}
					newSvc.Backends = append(newSvc.Backends, &newBackend)
				}
			}
			for beidx, be := range newSvc.Backends {
				if be.CapacityScaler == 0 {
					forceCapacity[beidx] = true
				}
			}
			if len(upsert.backends) > 0 {
				err = b.updateBackends(upsert.name, upsert.region, newSvc, forceCapacity, deleting)
			}
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// for sorting the backends to keep tests happy
func sortBackends(backends *[]compute.Backend) {
	sort.SliceStable(*backends, func(i, j int) bool {
		return (*backends)[i].Group < (*backends)[j].Group
	})
}

// ReconcileStatus takes the actual and intended AutonegStatus
// and returns sets of backends to remove, and to upsert
func ReconcileStatus(logger logr.Logger, project string, actual AutonegStatus, intended AutonegStatus) (removes, upserts map[string]map[string]Backends) {
	upserts = make(map[string]map[string]Backends, 0)
	removes = make(map[string]map[string]Backends, 0)

	// logger.Info("Reconciling statuses", "actual", fmt.Sprintf("%+v", actual), "intended", fmt.Sprintf("%+v", intended))

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

	// actualBE and intendedBE is a list of NEGs per port now
	var intendedBEKeys []string
	for k := range intendedBE {
		intendedBEKeys = append(intendedBEKeys, k)
	}
	sort.Strings(intendedBEKeys)

	for _, port := range intendedBEKeys {
		upserts[port] = make(map[string]Backends, len(intendedBE))
		removes[port] = make(map[string]Backends, len(intendedBE))

		groups := intendedBE[port]
		for bname, be := range intended.BackendServices[port] {
			upsert := Backends{name: be.Name, region: be.Region}

			var groupsKeys []string
			for k := range groups {
				groupsKeys = append(groupsKeys, k)
			}
			sort.Strings(groupsKeys)

			for _, i := range groupsKeys {
				be := intended.Backend(bname, port, i)
				upsert.backends = append(upsert.backends, be)
			}
			sortBackends(&upsert.backends)
			upserts[port][bname] = upsert

			remove := Backends{name: be.Name, region: be.Region}
			// test to see if we are changing backend services
			if _, ok := actual.BackendServices[port][bname]; ok {
				if actual.BackendServices[port][bname].Name == be.Name || actual.BackendServices[port][bname].Name == "" {
					// find backends to be deleted
					for a := range actualBE[port] {
						if _, ok := intendedBE[port][a]; !ok {
							rbe := actual.Backend(bname, port, a)
							remove.backends = append(remove.backends, rbe)
						}
					}
					sortBackends(&remove.backends)
					removes[port][bname] = remove
				} else {
					// moving to a different backend service means removing all actual backends
					remove.name = actual.BackendServices[port][bname].Name
					remove.region = actual.BackendServices[port][bname].Region
					for a := range actualBE[port] {
						rbe := actual.Backend(bname, port, a)
						remove.backends = append(remove.backends, rbe)
					}
					sortBackends(&remove.backends)
					removes[port][bname] = remove
				}
			} else {
				// add empty remove if adding to a mint backend service
				remove.name = intended.BackendServices[port][bname].Name
				remove.region = intended.BackendServices[port][bname].Region
				removes[port][bname] = remove
			}
		}

		// see if there are removed backend services
		for aname := range actual.BackendServices[port] {
			if _, ok := intended.BackendServices[port][aname]; !ok {
				be := actual.BackendServices[port][aname]
				remove := Backends{name: be.Name, region: be.Region}
				remove.name = actual.BackendServices[port][aname].Name
				remove.region = actual.BackendServices[port][aname].Region
				for a := range actualBE[port] {
					rbe := actual.Backend(aname, port, a)
					remove.backends = append(remove.backends, rbe)
				}
				sortBackends(&remove.backends)
				removes[port][aname] = remove

				upsert := Backends{name: be.Name, region: be.Region}
				upserts[port][aname] = upsert
			}
		}
	}

	// see if some configs were removed entirely
	for port, _ := range actual.BackendServices {
		if _, ok := intended.BackendServices[port]; !ok {
			if _, ok = removes[port]; !ok {
				removes[port] = make(map[string]Backends, len(actualBE))
			}
			for aname, be := range actual.BackendServices[port] {
				remove := Backends{name: be.Name, region: be.Region}
				remove.name = actual.BackendServices[port][aname].Name
				remove.region = actual.BackendServices[port][aname].Region
				for a := range actualBE[port] {
					rbe := actual.Backend(aname, port, a)
					remove.backends = append(remove.backends, rbe)
				}
				sortBackends(&remove.backends)
				removes[port][aname] = remove
			}
		}
	}
	return
}

func getGroup(project, zone, neg string) string {
	return fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/networkEndpointGroups/%s", project, zone, neg)
}

func validateOldConfig(cfg OldAutonegConfig) error {
	// do additional validation
	return nil
}

func validateNewConfig(config AutonegConfig) error {
	for _, cfgs := range config.BackendServices {
		for _, cfg := range cfgs {
			if cfg.InitialCapacity != nil {
				if *cfg.InitialCapacity < 0 || *cfg.InitialCapacity > 100 {
					return fmt.Errorf("initial_capacity for backend %q must be between 0 and 100 inclusive, but was %q; see https://cloud.google.com/load-balancing/docs/backend-service#capacity_scaler for details", cfg.Name, *cfg.InitialCapacity)
				}
			}
		}
	}

	for _, cfgs := range config.BackendServices {
		for _, cfg := range cfgs {
			if cfg.CapacityScaler != nil {
				if *cfg.CapacityScaler < 0 || *cfg.CapacityScaler > 100 {
					return fmt.Errorf("capacity_scaler for backend %q must be between 0 and 100 inclusive, but was %q; see https://cloud.google.com/load-balancing/docs/backend-service#capacity_scaler for details", cfg.Name, *cfg.CapacityScaler)
				}
			}
		}
	}

	return nil
}

func getStatuses(ctx context.Context, namespace string, name string, annotations map[string]string, r *ServiceReconciler) (s Statuses, valid bool, err error) {
	logger := log.FromContext(ctx)
	// Read the current cloud.google.com/neg annotation
	tmp, ok := annotations[negAnnotation]
	if ok {
		// Found a status, decode
		if err = json.Unmarshal([]byte(tmp), &s.negConfig); err != nil {
			err = fmt.Errorf("failed to decode neg annotation %s: %w", negAnnotation, err)
			return
		}
	}

	// Is this service using autoneg in new mode?
	oldOk := false
	tmp, newOk := annotations[autonegAnnotation] // controller.autoneg.dev/neg
	if newOk {
		valid = true

		var tempConfig AutonegConfigTemp
		if err = json.Unmarshal([]byte(tmp), &tempConfig); err != nil {
			err = fmt.Errorf("failed to decode autoneg annotation %s: %w", autonegAnnotation, err)
			return
		}

		tmpSync, syncOk := annotations[autonegSyncAnnotation]
		if syncOk {
			if err = json.Unmarshal([]byte(tmpSync), &s.syncConfig); err != nil {
				err = fmt.Errorf("failed to decode autoneg-sync annotation %s: %w", autonegSyncAnnotation, err)
				return
			}
		}

		s.config.BackendServices = make(map[string]map[string]AutonegNEGConfig, len(tempConfig.BackendServices))
		for port, cfgs := range tempConfig.BackendServices {
			s.config.BackendServices[port] = make(map[string]AutonegNEGConfig, len(cfgs))
			for _, cfg := range cfgs {
				if cfg.Name == "" || !r.AllowServiceName {
					// Default to name generated using serviceNameTemplate
					cfg.Name = generateServiceName(namespace, name, port, r.ServiceNameTemplate)
				}

				//Use defaults if rate and connections have not been set
				if cfg.Rate == 0 && cfg.Connections == 0 {
					if r.MaxRatePerEndpointDefault > 0 {
						cfg.Rate = r.MaxRatePerEndpointDefault
					} else {
						cfg.Connections = r.MaxConnectionsPerEndpointDefault
					}
				}

				s.config.BackendServices[port][cfg.Name] = cfg
			}
		}

		// Is this autoneg config valid?
		if err = validateNewConfig(s.config); err != nil {
			return
		}

		s.newConfig = true
	}

	// Does service has new auto neg status?
	tmp, statusOk := annotations[autonegStatusAnnotation]
	if statusOk {
		// Status annotation found but auto neg annotation not found set empty auto neg config
		if !newOk && r.DeregisterNEGsOnAnnotationRemoval {
			s.config = AutonegConfig{}
			valid = true
			s.newConfig = true
		}
		// Found a status, decode
		if err = json.Unmarshal([]byte(tmp), &s.status); err != nil {
			err = fmt.Errorf("failed to decode autoneg-status annotation %s: %w", autonegStatusAnnotation, err)
			return
		}
	}

	if !newOk && !statusOk {
		// Is this service using autoneg in legacy mode?
		tmp, oldOk = annotations[oldAutonegAnnotation]
		if oldOk {
			valid = true

			if err = json.Unmarshal([]byte(tmp), &s.oldConfig); err != nil {
				err = fmt.Errorf("failed to decode %s annotation %s: %w", oldAutonegAnnotation, tmp, err)
				return
			}

			// Default to the k8s service name
			if s.oldConfig.Name == "" {
				s.oldConfig.Name = name
			}

			// Is this autoneg config valid?
			if err = validateOldConfig(s.oldConfig); err != nil {
				return
			}

			// Convert the old configuration to a new style configuration
			s.config.BackendServices = make(map[string]map[string]AutonegNEGConfig, 1)
			if len(s.negConfig.ExposedPorts) == 1 {
				var firstPort string
				for k, _ := range s.negConfig.ExposedPorts {
					firstPort = k
					break
				}
				s.config.BackendServices[firstPort] = make(map[string]AutonegNEGConfig, 1)
				s.config.BackendServices[firstPort][s.oldConfig.Name] = AutonegNEGConfig{
					Name:        s.oldConfig.Name,
					Rate:        s.oldConfig.Rate,
					Connections: 0,
				}
			} else {
				err = fmt.Errorf("more than one port in %s, but autoneg configuration is for one or no ports", negAnnotation)
				return
			}
		}

		tmp, ok = annotations[oldAutonegStatusAnnotation]
		if ok {
			// Status annotation found but auto neg annotation not found set empty auto neg config
			if !oldOk && r.DeregisterNEGsOnAnnotationRemoval {
				s.oldConfig = OldAutonegConfig{}
				valid = true
			}
			// Found a status, decode
			if err = json.Unmarshal([]byte(tmp), &s.oldStatus); err != nil {
				err = fmt.Errorf("failed to decode %s annotation %s: %w", oldAutonegStatusAnnotation, tmp, err)
				return
			}
		}
	}

	tmp, ok = annotations[negStatusAnnotation]
	if ok {
		// Found a status, decode
		if err = json.Unmarshal([]byte(tmp), &s.negStatus); err != nil {
			return
		}
		// Check if we should use ServiceNetworkEndpointGroup custom resource to get the NEG zones.
		if r.UseSvcNeg {
			logger.Info("Getting zones using svcneg custom resources")
			var zones []string
			zones, err = zonesFromSvcNeg(ctx, r, namespace, &s.negStatus)
			if err != nil {
				return
			}
			// Update the zones.
			logger.Info("Got zones from svcnegs", "zones", zones)
			s.negStatus.Zones = zones
		}
	}

	return
}

func zonesFromSvcNeg(ctx context.Context, reader client.Reader, namespace string, negStatus *NEGStatus) ([]string, error) {
	logger := log.FromContext(ctx)
	zones := []string{}
	negsProcessed := map[string]bool{}
	for _, neg := range negStatus.NEGs {
		if _, ok := negsProcessed[neg]; ok {
			continue
		}
		negsProcessed[neg] = true
		svcNeg := v1beta1.ServiceNetworkEndpointGroup{}
		err := reader.Get(ctx, client.ObjectKey{
			Namespace: namespace, Name: neg,
		}, &svcNeg)
		if apierrors.IsNotFound(err) {
			logger.Info("SvcNeg not found", "neg", neg)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get svcneg %s: %w", neg, err)
		}
		for _, negRef := range svcNeg.Status.NetworkEndpointGroups {
			negZone := zone(negRef)
			if !slices.Contains(zones, negZone) {
				zones = append(zones, negZone)
			}
		}
	}
	return zones, nil
}

func zone(ref v1beta1.NegObjectReference) string {
	// Of the format: https://www.googleapis.com/compute/beta/projects/<project-id>/zones/<zone>/networkEndpointGroups/<neg>
	matches := zoneRE.FindStringSubmatch(ref.SelfLink)
	// The first submatch (index 0) is the entire matched string "zones/us-central1-c"
	// The second submatch (index 1) is the content of the first capturing group "us-central1-c"
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
