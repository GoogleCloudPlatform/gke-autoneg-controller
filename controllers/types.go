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

import "google.golang.org/api/compute/v1"

// NEGStatus specifies the output of the GKE NEG controller
// stored in the cloud.google.com/neg-status annotation
type NEGStatus struct {
	NEGs  map[string]string `json:"network_endpoint_groups"`
	Zones []string          `json:"zones"`
}

// AutonegConfig specifies the intended configuration of autoneg
// stored in the anthos.cft.dev/autoneg annotation
type OldAutonegConfig struct {
	Name string  `json:"name"`
	Rate float64 `json:"max_rate_per_endpoint"`
}

// AutonegConfig specifies the intended configuration of autoneg
// stored in the controller.autoneg.dev/neg annotation
type AutonegConfig struct {
	BackendServices map[string]map[string]AutonegNEGConfig `json:"backend_services"`
}

type AutonegConfigTemp struct {
	BackendServices map[string][]AutonegNEGConfig `json:"backend_services"`
}

// AutonegConfig specifies the intended configuration of autoneg
// stored in the controller.autoneg.dev/neg annotation
type AutonegNEGConfig struct {
	Name        string  `json:"name,omitempty"`
	Region      string  `json:"region,omitempty"`
	Regional    *bool   `json:"regional,omitempty"`
	Rate        float64 `json:"max_rate_per_endpoint,omitempty"`
	Connections float64 `json:"max_connections_per_endpoint,omitempty"`
}

// AutonegStatus specifies the reconciled status of autoneg
// stored in the anthos.cft.dev/autoneg-status annotation
type OldAutonegStatus struct {
	OldAutonegConfig
	NEGStatus
}

// AutonegStatus specifies the reconciled status of autoneg
// stored in the controller.autoneg.dev/neg annotation
type AutonegStatus struct {
	AutonegConfig
	NEGStatus
}

// Statuses represents the autoneg-relevant structs fetched from annotations
type Statuses struct {
	oldConfig OldAutonegConfig
	config    AutonegConfig
	oldStatus OldAutonegStatus
	status    AutonegStatus
	negStatus NEGStatus
	negConfig NEGConfig
	newConfig bool
}

// Backends specifies a name and list of compute.Backends
type Backends struct {
	name     string
	region   string
	backends []compute.Backend
}

// BackendController manages operations on a GCLB backend service
type BackendController struct {
	project  string
	location string
	s        *compute.Service
}

// NEGConfig specifies the configuration stored in
// in the cloud.google.com/neg annotation
type NEGConfig struct {
	ExposedPorts map[string]interface{} `json:"exposed_ports"`
}
