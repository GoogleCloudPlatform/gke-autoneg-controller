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

import "google.golang.org/api/compute/v1"

// NEGStatus specifies the output of the GKE NEG controller
// stored in the cloud.google.com/neg-status annotation
type NEGStatus struct {
	NEGs  map[string]string `json:"network_endpoint_groups"`
	Zones []string          `json:"zones"`
}

// AutonegConfig specifies the intended configuration of autoneg
// stored in the controller.autoneg.dev/neg annotation
type AutonegConfig struct {
	BackendServices map[string]map[string]AutonegNEGConfig `json:"backend_services"`
}

type AutonegConfigTemp struct {
	BackendServices map[string][]AutonegNEGConfig `json:"backend_services"`
}

// AutonegCustomMetric specifies the BackendCustomMetric for CUSTOM_METRICS balancing mode.
type AutonegCustomMetric struct {
	// DryRun: If true, the metric data is collected and reported to Cloud
	// Monitoring, but is not used for load balancing.
	DryRun bool `json:"dry_run,omitempty"`
	// MaxUtilization field on compute.BackendCustomMetric,
	// define a target utilization for the Custom Metrics balancing mode.
	// The valid range is [0.0, 1.0].
	MaxUtilization float64 `json:"max_utilization,omitempty"`
	// Name: Name of a custom utilization signal. The name must be 1-64 characters
	// long and match the regular expression a-z ([-_.a-z0-9]*[a-z0-9])? which
	// means the first character must be a lowercase letter, and all following
	// characters must be a dash, period, underscore, lowercase letter, or digit,
	// except the last character, which cannot be a dash, period, or underscore.
	// For usage guidelines, see Custom Metrics balancing mode. This field can only
	// be used for a global or regional backend service with the
	// loadBalancingScheme set to EXTERNAL_MANAGED, INTERNAL_MANAGED
	// INTERNAL_SELF_MANAGED.
	Name string `json:"name,omitempty"`
}

// AutonegNEGConfig specifies the intended configuration of autoneg
// stored in the controller.autoneg.dev/neg annotation
type AutonegNEGConfig struct {
	Name            string                `json:"name,omitempty"`
	Region          string                `json:"region,omitempty"`
	Rate            float64               `json:"max_rate_per_endpoint,omitempty"`
	Connections     float64               `json:"max_connections_per_endpoint,omitempty"`
	CustomMetrics   []AutonegCustomMetric `json:"custom_metrics,omitempty"`
	InitialCapacity *int32                `json:"initial_capacity,omitempty"`
	CapacityScaler  *int32                `json:"capacity_scaler,omitempty"`
}

// AutonegSyncConfig specifies additional configuration which to sync
type AutonegSyncConfig struct {
	CapacityScaler *bool `json:"capacity_scaler,omitempty"`
}

// AutonegStatus specifies the reconciled status of autoneg
// stored in the controller.autoneg.dev/neg annotation
type AutonegStatus struct {
	AutonegConfig
	NEGStatus
	AutonegSyncConfig *AutonegSyncConfig `json:"sync,omitempty"`
}

// Statuses represents the autoneg-relevant structs fetched from annotations
type Statuses struct {
	config     AutonegConfig
	status     AutonegStatus
	negStatus  NEGStatus
	negConfig  NEGConfig
	syncConfig *AutonegSyncConfig
}

// Backends specifies a name and list of compute.Backends
type Backends struct {
	name     string
	region   string
	backends []compute.Backend
}

// ProdBackendController implements BackendController and manages operations on a GCLB backend service
type ProdBackendController struct {
	project string
	s       *compute.Service
}

// NEGConfig specifies the configuration stored in
// in the cloud.google.com/neg annotation
type NEGConfig struct {
	ExposedPorts map[string]interface{} `json:"exposed_ports"`
}
