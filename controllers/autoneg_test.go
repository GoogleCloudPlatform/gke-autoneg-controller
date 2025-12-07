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
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"google.golang.org/api/option"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"google.golang.org/api/compute/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	malformedJSON         = `{`
	validConfig           = `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100,"initial_capacity":100}],"443":[{"name":"https-be","max_connections_per_endpoint":1000,"initial_capacity":0}]}}`
	brokenConfig          = `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":"100"}],"443":[{"name":"https-be","max_connections_per_endpoint":1000}}}`
	validMultiConfig      = `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100},{"name":"http-ilb-be","max_rate_per_endpoint":100}],"443":[{"name":"https-be","max_connections_per_endpoint":1000},{"name":"https-ilb-be","max_connections_per_endpoint":1000}]}}`
	validConfigWoName     = `{"backend_services":{"80":[{"max_rate_per_endpoint":100}],"443":[{"max_connections_per_endpoint":1000}]}}`
	invalidCapacityConfig = `{"backend_services":{"443":[{"max_connections_per_endpoint":1000,"initial_capacity":500}]}}`
	validWithCustomMetric = `{"backend_services":{"80":[{"name":"http-be","custom_metrics":[{"name": "orca.named_metrics.cool_one", "max_utilization": 0.8}]}]}}`
	invalidCustomMetric   = `{"backend_services":{"80":[{"name":"http-be","custom_metrics":[{"name": "orca.named_metrics.cool_one", "max_utilization": 8.0}]}]}}`

	validStatus        = `{}`
	validAutonegConfig = `{}`
	validAutonegStatus = `{}`
	validSyncConfig    = `{"capacity_scaler":true}`
	invalidSyncConfig  = `{"capacity_scaler":"foobar"}`
	invalidStatus      = `{`

	validNegConfig   = `{"exposed_ports": {"80":{"name":"test"}}}`
	wrongNegConfig   = `{"exposed_ports": {}}`
	tooManyNegConfig = `{"exposed_ports": {"80":{"name":"test"},"443":{"name":"tls"}}}`
	brokenNegConfig  = `{"exposed_ports": {}`
)

var statusTests = []struct {
	name        string
	annotations map[string]string
	valid       bool
	err         bool
}{
	{
		"not using autoneg",
		map[string]string{},
		false,
		false,
	},
	{
		"autoneg with malformed config",
		map[string]string{
			autonegAnnotation: malformedJSON,
		},
		true,
		true,
	},
	{
		"autoneg with broken config",
		map[string]string{
			autonegAnnotation: brokenConfig,
		},
		true,
		true,
	},
	{
		"valid autoneg",
		map[string]string{
			autonegAnnotation: validConfig,
		},
		true,
		false,
	},
	{
		"valid multi autoneg",
		map[string]string{
			autonegAnnotation: validMultiConfig,
		},
		true,
		false,
	},
	{
		"valid autoneg with invalid status",
		map[string]string{
			autonegAnnotation:       validConfig,
			autonegStatusAnnotation: malformedJSON,
		},
		true,
		true,
	},
	{
		"valid autoneg with valid status",
		map[string]string{
			autonegAnnotation:       validConfig,
			autonegStatusAnnotation: validStatus,
		},
		true,
		false,
	},
	{
		"valid autoneg with valid neg status",
		map[string]string{
			autonegAnnotation:   validConfig,
			negStatusAnnotation: validStatus,
		},
		true,
		false,
	},
	{
		"valid autoneg without neg name",
		map[string]string{
			autonegAnnotation:   validConfigWoName,
			negStatusAnnotation: validStatus,
		},
		true,
		false,
	},
	{
		"invalid capacity config with valid neg status",
		map[string]string{
			autonegAnnotation:   invalidCapacityConfig,
			negStatusAnnotation: validStatus,
		},
		true,
		true,
	},
	{
		"valid autoneg config with valid neg status",
		map[string]string{
			autonegAnnotation:   validAutonegConfig,
			negStatusAnnotation: validStatus,
		},
		true,
		false,
	},
	{
		"valid autoneg config with valid neg status and sync status",
		map[string]string{
			autonegAnnotation:     validAutonegConfig,
			negStatusAnnotation:   validStatus,
			autonegSyncAnnotation: validSyncConfig,
		},
		true,
		false,
	},
	{
		"valid autoneg config with valid neg status and invalid sync status",
		map[string]string{
			autonegAnnotation:     validAutonegConfig,
			negStatusAnnotation:   validStatus,
			autonegSyncAnnotation: invalidSyncConfig,
		},
		true,
		true,
	},
	{
		"valid autoneg status without autoneg",
		map[string]string{
			autonegStatusAnnotation: validStatus,
		},
		true,
		false,
	},
	{
		"valid autoneg config with custom metrics",
		map[string]string{
			autonegAnnotation: validWithCustomMetric,
		},
		true,
		false,
	},
	{
		"invalid autoneg custom metric treshold",
		map[string]string{
			autonegAnnotation: invalidCustomMetric,
		},
		true,
		true,
	},
}

func TestGetStatuses(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate:               "{namespace}-{name}-{port}-{hash}",
		AllowServiceName:                  true,
		DeregisterNEGsOnAnnotationRemoval: true,
	}
	for _, st := range statusTests {
		_, valid, err := getStatuses(context.Background(), "ns", "test", st.annotations, &serviceReconciler)
		if err != nil && !st.err {
			t.Errorf("Set %q: expected no error, got one: %v", st.name, err)
		}
		if err == nil && st.err {
			t.Errorf("Set %q: expected error, got none", st.name)
		}
		if !valid && st.valid {
			t.Errorf("Set %q: expected autoneg config, got none", st.name)
		}
		if valid && !st.valid {
			t.Errorf("Set %q: expected no autoneg config, got one", st.name)
		}
	}
}

func TestGetStatusesServiceNameNotAllowed(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate: "{namespace}-{name}-{port}",
		AllowServiceName:    false,
	}
	validConf := `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}]}}`
	statuses, valid, err := getStatuses(context.Background(), "ns", "test", map[string]string{autonegAnnotation: validConf}, &serviceReconciler)
	if err != nil {
		t.Errorf("Expected no error, got one: %v", err)
	}
	if !valid {
		t.Errorf("Expected autoneg config, got none")
	}
	_, ok := statuses.config.BackendServices["80"]["ns-test-80"]
	if !ok {
		t.Errorf("Expected service config for ns-test-80 but got none, service statuses: \n%v", statuses.config.BackendServices)
	}
}

func TestGetStatusesServiceNameAllowed(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate: "{namespace}-{name}-{port}",
		AllowServiceName:    true,
	}
	validConf := `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}]}}`
	statuses, valid, err := getStatuses(context.Background(), "ns", "test", map[string]string{autonegAnnotation: validConf}, &serviceReconciler)
	if err != nil {
		t.Errorf("Expected no error, got one: %v", err)
	}
	if !valid {
		t.Errorf("Expected autoneg config, got none")
	}
	_, ok := statuses.config.BackendServices["80"]["http-be"]
	if !ok {
		t.Errorf("Expected service config for http-be but got none, service statuses: \n%v", statuses.config.BackendServices)
	}
}

func TestGetStatusesOnlyAutonegStatusAnnotation(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate:               "{namespace}-{name}-{port}-{hash}",
		AllowServiceName:                  true,
		DeregisterNEGsOnAnnotationRemoval: true,
	}
	statuses, valid, err := getStatuses(context.Background(), "ns", "test", map[string]string{autonegStatusAnnotation: validAutonegStatus}, &serviceReconciler)
	if err != nil {
		t.Errorf("Expected no error, got one: %v", err)
	}
	if !valid {
		t.Errorf("Expected autoneg status config, got none")
	}
	if statuses.config.BackendServices != nil {
		t.Errorf("Expected nil backend services")
	}
}

func TestDefaultMaxRatePerEndpointWhenOverrideIsSet(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate:       "{namespace}-{name}-{port}",
		AllowServiceName:          true,
		MaxRatePerEndpointDefault: 1234,
	}
	validConf := `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}]}}`
	statuses, valid, err := getStatuses(context.Background(), "ns", "test", map[string]string{autonegAnnotation: validConf}, &serviceReconciler)
	if err != nil {
		t.Errorf("Expected no error, got one: %v", err)
	}
	if !valid {
		t.Errorf("Expected autoneg config, got none")
	}
	cfg, ok := statuses.config.BackendServices["80"]["http-be"]
	if !ok {
		t.Errorf("Expected service config for http-be but got none, service statuses: \n%v", statuses.config.BackendServices)
	}
	if cfg.Rate != 100 {
		t.Errorf("Expected max_rate_per_endpoint to be 100 but got: \n%v", cfg.Rate)
	}
}

func TestDefaultMaxRatePerEndpointWhenOverrideIsNotSet(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate:       "{namespace}-{name}-{port}",
		AllowServiceName:          true,
		MaxRatePerEndpointDefault: 1234,
	}
	validConf := `{"backend_services":{"80":[{"name":"http-be"}]}}`
	statuses, valid, err := getStatuses(context.Background(), "ns", "test", map[string]string{autonegAnnotation: validConf}, &serviceReconciler)
	if err != nil {
		t.Errorf("Expected no error, got one: %v", err)
	}
	if !valid {
		t.Errorf("Expected autoneg config, got none")
	}
	cfg, ok := statuses.config.BackendServices["80"]["http-be"]
	if !ok {
		t.Errorf("Expected service config for http-be but got none, service statuses: \n%v", statuses.config.BackendServices)
	}
	if cfg.Rate != 1234 {
		t.Errorf("Expected max_rate_per_endpoint to be 1234 but got: \n%v", cfg.Rate)
	}
}

func TestDefaultConnectionPerEndpointWhenOverrideIsSet(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate:              "{namespace}-{name}-{port}",
		AllowServiceName:                 true,
		MaxConnectionsPerEndpointDefault: 1234,
	}
	validConf := `{"backend_services":{"80":[{"name":"http-be","max_connections_per_endpoint":100}]}}`
	statuses, valid, err := getStatuses(context.Background(), "ns", "test", map[string]string{autonegAnnotation: validConf}, &serviceReconciler)
	if err != nil {
		t.Errorf("Expected no error, got one: %v", err)
	}
	if !valid {
		t.Errorf("Expected autoneg config, got none")
	}
	cfg, ok := statuses.config.BackendServices["80"]["http-be"]
	if !ok {
		t.Errorf("Expected service config for http-be but got none, service statuses: \n%v", statuses.config.BackendServices)
	}
	if cfg.Connections != 100 {
		t.Errorf("Expected max_rate_per_endpoint to be 100 but got: \n%v", cfg.Rate)
	}
}

func TestDefaultMaxConnectionsEndpointWhenOverrideIsNotSet(t *testing.T) {
	var serviceReconciler = ServiceReconciler{
		ServiceNameTemplate:              "{namespace}-{name}-{port}",
		AllowServiceName:                 true,
		MaxConnectionsPerEndpointDefault: 1234,
	}
	validConf := `{"backend_services":{"80":[{"name":"http-be"}]}}`
	statuses, valid, err := getStatuses(context.Background(), "ns", "test", map[string]string{autonegAnnotation: validConf}, &serviceReconciler)
	if err != nil {
		t.Errorf("Expected no error, got one: %v", err)
	}
	if !valid {
		t.Errorf("Expected autoneg config, got none")
	}
	cfg, ok := statuses.config.BackendServices["80"]["http-be"]
	if !ok {
		t.Errorf("Expected service config for http-be but got none, service statuses: \n%v", statuses.config.BackendServices)
	}
	if cfg.Connections != 1234 {
		t.Errorf("Expected max_connections_per_endpoint to be 1234 but got: \n%v", cfg.Rate)
	}
}

func TestValidateNewConfig(t *testing.T) {
	tests := []struct {
		name                   string
		config                 AutonegConfig
		err                    bool
		expectedCapacityScaler float64
		expectedBalancingMode  string
	}{
		{
			name:                   "default config",
			config:                 AutonegConfig{},
			err:                    false,
			expectedCapacityScaler: 1,
			expectedBalancingMode:  "CONNECTION",
		},
		{
			name: "negative initial_capacity",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name:            "http-be",
							Connections:     100,
							InitialCapacity: ptr.To(int32(-10)),
						},
					},
				},
			},
			err:                    true,
			expectedCapacityScaler: 1,
			expectedBalancingMode:  "CONNECTION",
		},
		{
			name: "large initial capacity",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name:            "http-be",
							Connections:     100,
							InitialCapacity: ptr.To(int32(5000)),
						},
					},
				},
			},
			err:                    true,
			expectedCapacityScaler: 1,
			expectedBalancingMode:  "CONNECTION",
		},
		{
			name: "zero initial capacity",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name:            "http-be",
							Connections:     100,
							InitialCapacity: ptr.To(int32(0)),
						},
					},
				},
			},
			err:                    false,
			expectedCapacityScaler: 0,
			expectedBalancingMode:  "CONNECTION",
		},
		{
			name: "half initial capacity",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name:            "http-be",
							Connections:     100,
							InitialCapacity: ptr.To(int32(50)),
						},
					},
				},
			},
			err:                    false,
			expectedCapacityScaler: 0.5,
			expectedBalancingMode:  "CONNECTION",
		},
		{
			name: "max initial capacity",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name:            "http-be",
							Rate:            100,
							InitialCapacity: ptr.To(int32(100)),
						},
					},
				},
			},
			err:                    false,
			expectedCapacityScaler: 1,
			expectedBalancingMode:  "RATE",
		},
		{
			name: "updated capacity",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name:            "http-be",
							Rate:            100,
							InitialCapacity: ptr.To(int32(10)),
							CapacityScaler:  ptr.To(int32(42)),
						},
					},
				},
			},
			err:                    false,
			expectedCapacityScaler: 0.42,
			expectedBalancingMode:  "RATE",
		},
		{
			name: "single custom metric",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name: "http-be",
							CustomMetrics: []AutonegCustomMetric{
								{
									Name:           "cool_one",
									MaxUtilization: 0.5,
								},
							},
							InitialCapacity: ptr.To(int32(10)),
							CapacityScaler:  ptr.To(int32(42)),
						},
					},
				},
			},
			err:                    false,
			expectedCapacityScaler: 0.42,
			expectedBalancingMode:  "CUSTOM_METRICS",
		},
		{
			name: "three custom metrics with dry run",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name: "http-be",
							CustomMetrics: []AutonegCustomMetric{
								{
									DryRun:         true,
									Name:           "cool_1",
									MaxUtilization: 0.5,
								},
								{
									DryRun:         true,
									Name:           "cool_2",
									MaxUtilization: 0.5,
								},
								{
									DryRun:         true,
									Name:           "cool_3",
									MaxUtilization: 0.5,
								},
							},
							InitialCapacity: ptr.To(int32(10)),
							CapacityScaler:  ptr.To(int32(42)),
						},
					},
				},
			},
			err:                    false,
			expectedCapacityScaler: 0.42,
			expectedBalancingMode:  "CUSTOM_METRICS",
		},
		{
			name: "too many custom metrics without dry run",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name: "http-be",
							CustomMetrics: []AutonegCustomMetric{
								{Name: "cool_1"},
								{DryRun: true, Name: "cool_2"},
								{DryRun: true, Name: "cool_3"},
							},
							InitialCapacity: ptr.To(int32(10)),
							CapacityScaler:  ptr.To(int32(42)),
						},
					},
				},
			},
			err:                    true,
			expectedCapacityScaler: 0.42,
			expectedBalancingMode:  "CUSTOM_METRICS",
		},
		{
			name: "too many custom metrics",
			config: AutonegConfig{
				BackendServices: map[string]map[string]AutonegNEGConfig{
					"80": {
						"http-be": {
							Name: "http-be",
							CustomMetrics: []AutonegCustomMetric{
								{Name: "cool_1"},
								{Name: "cool_2"},
								{Name: "cool_3"},
								{Name: "cool_4"},
							},
							InitialCapacity: ptr.To(int32(10)),
							CapacityScaler:  ptr.To(int32(42)),
						},
					},
				},
			},
			err:                    true,
			expectedCapacityScaler: 0.42,
			expectedBalancingMode:  "CUSTOM_METRICS",
		},
	}

	for _, ct := range tests {
		err := validateConfig(ct.config)
		if err == nil && ct.err {
			t.Errorf("Set %q: expected error, got none", ct.name)
		}
		if err != nil && !ct.err {
			t.Errorf("Set %q: expected no error, got one: %v", ct.name, err)
		}

		// The compute.Backend object should have a float64 value in
		// the range [0.0, 1.0]
		status := AutonegStatus{AutonegConfig: ct.config}
		beConfig := status.Backend("http-be", "80", "group")
		if beConfig.CapacityScaler < 0 || beConfig.CapacityScaler > 1 {
			t.Errorf("Set %q: expected capacityScaler in [0.0, 1.0], got %f", ct.name, beConfig.CapacityScaler)
		}

		// Actual value should be within 1e-9 of expected
		if diff := math.Abs(beConfig.CapacityScaler - ct.expectedCapacityScaler); diff > 1e-9 {
			t.Errorf("Set %q: expected CapacityScaler of %f, got %f (diff %f)", ct.name, ct.expectedCapacityScaler, beConfig.CapacityScaler, diff)
		}

		if beConfig.BalancingMode != ct.expectedBalancingMode {
			t.Errorf("Set %q: expected balacing mode %q, got: %q", ct.name, ct.expectedBalancingMode, beConfig.BalancingMode)
		}
	}
}

func relevantCopy(a compute.Backend) compute.Backend {
	b := compute.Backend{}
	b.Group = a.Group
	b.MaxRatePerEndpoint = a.MaxRatePerEndpoint
	b.MaxConnectionsPerEndpoint = a.MaxConnectionsPerEndpoint
	if len(a.CustomMetrics) > 0 {
		b.CustomMetrics = slices.Collect(func(yield func(*compute.BackendCustomMetric) bool) {
			for _, acm := range a.CustomMetrics {
				bcm := *acm
				bcm.ForceSendFields = slices.Collect(slices.Values(acm.ForceSendFields))
				bcm.NullFields = slices.Collect(slices.Values(acm.NullFields))
				if !yield(&bcm) {
					return
				}
			}
		})
	}
	return b
}

func (b Backends) isEqual(ob Backends) bool {
	if b.name != ob.name {
		return false
	}
	newB := []compute.Backend{}
	for _, be := range b.backends {
		newB = append(newB, relevantCopy(be))
	}
	newOB := []compute.Backend{}
	for _, be := range ob.backends {
		newOB = append(newOB, relevantCopy(be))
	}
	return reflect.DeepEqual(newB, newOB)
}

var (
	fakeNeg     = "neg_name"
	fakeNeg2    = "neg_name2"
	fakeProject = "project"
	negStatus   = NEGStatus{
		NEGs:  map[string]string{"80": fakeNeg},
		Zones: []string{"zone1", "zone2"},
	}
	negStatusMulti = NEGStatus{
		NEGs:  map[string]string{"80": fakeNeg, "443": fakeNeg2},
		Zones: []string{"zone1", "zone2"},
	}

	// empty state
	configEmpty   = AutonegConfig{}
	statusEmpty   = AutonegStatus{AutonegConfig: configEmpty}
	backendsEmpty = map[string]map[string]Backends{}

	// initial state
	statusInitial = AutonegStatus{AutonegConfig: configBasic}
	backendsNone  = map[string]map[string]Backends{"80": map[string]Backends{"test": Backends{name: "test"}}}

	// basic state
	configBasicPort80        = AutonegNEGConfig{Name: "test", Rate: 100}
	configBasicPort80Slice   = map[string]AutonegNEGConfig{"test": configBasicPort80}
	configBasicPort80Backend = map[string]map[string]AutonegNEGConfig{"80": configBasicPort80Slice}
	configBasic              = AutonegConfig{BackendServices: configBasicPort80Backend}

	// custom metric state
	configCustomMetricPort80 = AutonegNEGConfig{Name: "test", CustomMetrics: []AutonegCustomMetric{
		{Name: "cool_one", MaxUtilization: 0.8}, {Name: "cool_two", MaxUtilization: 0.6},
	}}
	configCMPort80Slice   = map[string]AutonegNEGConfig{"test": configCustomMetricPort80}
	configCMPort80Backend = map[string]map[string]AutonegNEGConfig{"80": configCMPort80Slice}
	configCustomMetric    = AutonegConfig{BackendServices: configCMPort80Backend}

	statusBasicWithNEGs = AutonegStatus{
		AutonegConfig: configBasic,
		NEGStatus:     negStatus,
	}
	statusBasicWithEmptyNEGs = AutonegStatus{
		AutonegConfig: configBasic,
		NEGStatus:     NEGStatus{},
	}

	statusCMWithNEGs = AutonegStatus{
		AutonegConfig: configCustomMetric,
		NEGStatus:     negStatus,
	}
	statusCMWithEmptyNEGs = AutonegStatus{
		AutonegConfig: configCustomMetric,
		NEGStatus:     NEGStatus{},
	}

	backendsBasicWithNEGs = map[string]map[string]Backends{"80": {"test": {name: "test", backends: []compute.Backend{
		statusBasicWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusBasicWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}}

	backendsCMWithNEGs = map[string]map[string]Backends{"80": {"test": {name: "test", backends: []compute.Backend{
		statusCMWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusCMWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}}

	// value changed state
	configValueChangePort80        = AutonegNEGConfig{Name: "test", Rate: 200}
	configValueChangePort80Slice   = map[string]AutonegNEGConfig{"test": configValueChangePort80}
	configValueChangePort80Backend = map[string]map[string]AutonegNEGConfig{"80": configValueChangePort80Slice}
	configValueChange              = AutonegConfig{BackendServices: configValueChangePort80Backend}
	statusValueChangeWithNEGs      = AutonegStatus{
		AutonegConfig: configValueChange,
		NEGStatus:     negStatus,
	}
	backendsValueChangeWithNEGs = map[string]map[string]Backends{"80": {"test": {name: "test", backends: []compute.Backend{
		statusValueChangeWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusValueChangeWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}}

	// named changed state
	configNameChangePort80        = AutonegNEGConfig{Name: "changed", Rate: 100}
	configNameChangePort80Slice   = map[string]AutonegNEGConfig{"changed": configNameChangePort80}
	configNameChangePort80Backend = map[string]map[string]AutonegNEGConfig{"80": configNameChangePort80Slice}
	configNameChange              = AutonegConfig{BackendServices: configNameChangePort80Backend}
	statusNameChangeWithNEGs      = AutonegStatus{
		AutonegConfig: configNameChange,
		NEGStatus:     negStatus,
	}
	backendsNameChangeWithNEGs = map[string]map[string]Backends{"80": map[string]Backends{"changed": Backends{name: "changed", backends: []compute.Backend{
		statusNameChangeWithNEGs.Backend("changed", "80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusNameChangeWithNEGs.Backend("changed", "80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}}

	// multi-port/multi-backend state
	configMultiPort80Backend1  = AutonegNEGConfig{Name: "multi", Rate: 100}
	configMultiPort80Backend2  = AutonegNEGConfig{Name: "multi-ilb", Region: "europe-west4", Connections: 100}
	configMultiPort443Backend1 = AutonegNEGConfig{Name: "multi2", Rate: 100}
	configMultiPort443Backend2 = AutonegNEGConfig{Name: "multi2-ilb", Region: "europe-west4", Connections: 100}
	configMultiPort80Slice     = map[string]AutonegNEGConfig{"multi": configMultiPort80Backend1, "multi-ilb": configMultiPort80Backend2}
	configMultiPort443Slice    = map[string]AutonegNEGConfig{"multi2": configMultiPort443Backend1, "multi2-ilb": configMultiPort443Backend2}
	configMultiPortBackend     = map[string]map[string]AutonegNEGConfig{"80": configMultiPort80Slice, "443": configMultiPort443Slice}
	configMulti                = AutonegConfig{BackendServices: configMultiPortBackend}

	statusMultiWithNEGs = AutonegStatus{
		AutonegConfig: configMulti,
		NEGStatus:     negStatusMulti,
	}
	backendsMultiWithNEGs = map[string]map[string]Backends{
		"80": map[string]Backends{"multi": Backends{name: "multi", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi", "80", getGroup(fakeProject, "zone1", fakeNeg)),
			statusMultiWithNEGs.Backend("multi", "80", getGroup(fakeProject, "zone2", fakeNeg)),
		}}, "multi-ilb": Backends{name: "multi-ilb", region: "europe-west4", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi-ilb", "80", getGroup(fakeProject, "zone1", fakeNeg)),
			statusMultiWithNEGs.Backend("multi-ilb", "80", getGroup(fakeProject, "zone2", fakeNeg)),
		}}}, "443": map[string]Backends{"multi2": Backends{name: "multi2", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi2", "443", getGroup(fakeProject, "zone1", fakeNeg2)),
			statusMultiWithNEGs.Backend("multi2", "443", getGroup(fakeProject, "zone2", fakeNeg2)),
		}}, "multi2-ilb": Backends{name: "multi2-ilb", region: "europe-west4", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi2-ilb", "443", getGroup(fakeProject, "zone1", fakeNeg2)),
			statusMultiWithNEGs.Backend("multi2-ilb", "443", getGroup(fakeProject, "zone2", fakeNeg2)),
		}}},
	}

	configMultiChangePort80Slice  = map[string]AutonegNEGConfig{"multi": configMultiPort80Backend1}
	configMultiChangePort443Slice = map[string]AutonegNEGConfig{"multi2": configMultiPort443Backend1}
	configMultiChangePortBackend  = map[string]map[string]AutonegNEGConfig{"80": configMultiChangePort80Slice, "443": configMultiChangePort443Slice}
	configMultiChange             = AutonegConfig{BackendServices: configMultiChangePortBackend}

	statusMultiChangeWithNEGs = AutonegStatus{
		AutonegConfig: configMultiChange,
		NEGStatus:     negStatusMulti,
	}
	backendsMultiChangeWithNEGs = map[string]map[string]Backends{
		"80": map[string]Backends{"multi": Backends{name: "multi", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi", "80", getGroup(fakeProject, "zone1", fakeNeg)),
			statusMultiWithNEGs.Backend("multi", "80", getGroup(fakeProject, "zone2", fakeNeg)),
		}}, "multi-ilb": Backends{name: "multi-ilb", region: "europe-west4", backends: []compute.Backend{}}}, "443": map[string]Backends{"multi2": Backends{name: "multi2", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi2", "443", getGroup(fakeProject, "zone1", fakeNeg2)),
			statusMultiWithNEGs.Backend("multi2", "443", getGroup(fakeProject, "zone2", fakeNeg2)),
		}}, "multi2-ilb": Backends{name: "multi2-ilb", region: "europe-west4", backends: []compute.Backend{}}},
	}
	backendsMultiChangeWithNEGsRemoves = map[string]map[string]Backends{
		"80": map[string]Backends{"multi": Backends{name: "multi", backends: []compute.Backend{}}, "multi-ilb": Backends{name: "multi-ilb", region: "europe-west4", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi-ilb", "80", getGroup(fakeProject, "zone1", fakeNeg)),
			statusMultiWithNEGs.Backend("multi-ilb", "80", getGroup(fakeProject, "zone2", fakeNeg)),
		}}}, "443": map[string]Backends{"multi2": Backends{name: "multi2", backends: []compute.Backend{}}, "multi2-ilb": Backends{name: "multi2-ilb", region: "europe-west4", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi2-ilb", "443", getGroup(fakeProject, "zone1", fakeNeg2)),
			statusMultiWithNEGs.Backend("multi2-ilb", "443", getGroup(fakeProject, "zone2", fakeNeg2)),
		}}},
	}

	// remove first backend
	configMultiChangeFirstPort80Slice  = map[string]AutonegNEGConfig{"multi-ilb": configMultiPort80Backend2}
	configMultiChangeFirstPort443Slice = map[string]AutonegNEGConfig{"multi2-ilb": configMultiPort443Backend2}
	configMultiChangeFirstPortBackend  = map[string]map[string]AutonegNEGConfig{"80": configMultiChangeFirstPort80Slice, "443": configMultiChangeFirstPort443Slice}
	configMultiChangeFirst             = AutonegConfig{BackendServices: configMultiChangeFirstPortBackend}

	statusMultiChangeFirstWithNEGs = AutonegStatus{
		AutonegConfig: configMultiChangeFirst,
		NEGStatus:     negStatusMulti,
	}
	backendsMultiChangeFirstWithNEGs = map[string]map[string]Backends{
		"80": map[string]Backends{"multi": Backends{name: "multi", backends: []compute.Backend{}}, "multi-ilb": Backends{name: "multi-ilb", region: "europe-west4", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi-ilb", "80", getGroup(fakeProject, "zone1", fakeNeg)),
			statusMultiWithNEGs.Backend("multi-ilb", "80", getGroup(fakeProject, "zone2", fakeNeg)),
		}}}, "443": map[string]Backends{"multi2": Backends{name: "multi2", backends: []compute.Backend{}}, "multi2-ilb": Backends{name: "multi2-ilb", region: "europe-west4", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi2-ilb", "443", getGroup(fakeProject, "zone1", fakeNeg2)),
			statusMultiWithNEGs.Backend("multi2-ilb", "443", getGroup(fakeProject, "zone2", fakeNeg2)),
		}}},
	}
	backendsMultiChangeFirstWithNEGsRemoves = map[string]map[string]Backends{
		"80": map[string]Backends{"multi": Backends{name: "multi", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi", "80", getGroup(fakeProject, "zone1", fakeNeg)),
			statusMultiWithNEGs.Backend("multi", "80", getGroup(fakeProject, "zone2", fakeNeg)),
		}}, "multi-ilb": Backends{name: "multi-ilb", region: "europe-west4", backends: []compute.Backend{}}}, "443": map[string]Backends{"multi2": Backends{name: "multi2", backends: []compute.Backend{
			statusMultiWithNEGs.Backend("multi2", "443", getGroup(fakeProject, "zone1", fakeNeg2)),
			statusMultiWithNEGs.Backend("multi2", "443", getGroup(fakeProject, "zone2", fakeNeg2)),
		}}, "multi2-ilb": Backends{name: "multi2-ilb", region: "europe-west4", backends: []compute.Backend{}}},
	}
)

var reconcileTests = []struct {
	name     string
	actual   AutonegStatus
	intended AutonegStatus
	removes  map[string]map[string]Backends
	upserts  map[string]map[string]Backends
}{
	{
		"initial to basic",
		statusInitial,
		statusBasicWithNEGs,
		backendsNone,
		backendsBasicWithNEGs,
	},
	{
		"basic to value changed",
		statusBasicWithNEGs,
		statusValueChangeWithNEGs,
		backendsNone,
		backendsValueChangeWithNEGs,
	},
	{
		"basic to value changed",
		statusBasicWithNEGs,
		statusValueChangeWithNEGs,
		backendsNone,
		backendsValueChangeWithNEGs,
	},
	{
		"empty to multiport",
		statusEmpty,
		statusMultiWithNEGs,
		backendsEmpty,
		backendsMultiWithNEGs,
	},
	{
		"multiport remove second backend",
		statusMultiWithNEGs,
		statusMultiChangeWithNEGs,
		backendsMultiChangeWithNEGsRemoves,
		backendsMultiChangeWithNEGs,
	},
	{
		"multiport remove first backend",
		statusMultiWithNEGs,
		statusMultiChangeFirstWithNEGs,
		backendsMultiChangeFirstWithNEGsRemoves,
		backendsMultiChangeFirstWithNEGs,
	},
	{
		"basic to empty config",
		statusBasicWithNEGs,
		statusEmpty,
		backendsBasicWithNEGs,
		backendsEmpty,
	},
	{
		"custom metric to empty config",
		statusCMWithNEGs,
		statusEmpty,
		backendsCMWithNEGs,
		backendsEmpty,
	},
	{
		"empty config to custom metric",
		statusEmpty,
		statusCMWithNEGs,
		backendsEmpty,
		backendsCMWithNEGs,
	},
	{
		"custom metric to basic",
		statusCMWithNEGs,
		statusBasicWithNEGs,
		backendsNone,
		backendsBasicWithNEGs,
	},
	{
		"basic to custom metric",
		statusBasicWithNEGs,
		statusCMWithNEGs,
		backendsNone,
		backendsCMWithNEGs,
	},

	// ReconcileStatus will leave the removed/replaced status as a backend service with zero backend,
	// tweak the expected results accordingly.
	// Such zero backends BackendService is managed in the API caller to reset or deleting the BackendService
	{
		"basic to name changed",
		statusBasicWithNEGs,
		statusNameChangeWithNEGs,

		// backendsBasicWithNEGs, // plus an empty backends named changed
		map[string]map[string]Backends{"80": maps.Collect(func(yield func(string, Backends) bool) {
			for k, v := range backendsBasicWithNEGs["80"] {
				if !yield(k, v) {
					return
				}
			}
			for k := range backendsNameChangeWithNEGs["80"] {
				if !yield(k, Backends{name: k}) {
					return
				}
			}
		})},

		// backendsNameChangeWithNEGs, // plus an empty backends named test
		map[string]map[string]Backends{"80": maps.Collect(func(yield func(string, Backends) bool) {
			for k, v := range backendsNameChangeWithNEGs["80"] {
				if !yield(k, v) {
					return
				}
			}
			for k := range backendsBasicWithNEGs["80"] {
				if !yield(k, Backends{name: k}) {
					return
				}
			}
		})},
	},
}

func TestReconcileStatuses(t *testing.T) {
	logger := log.FromContext(context.TODO())
	for _, rt := range reconcileTests {
		removes, upserts := ReconcileStatus(logger, fakeProject, rt.actual, rt.intended)
		for port := range rt.removes {
			if _, ok := removes[port]; !ok {
				t.Errorf("Set %q: Removed port %s backends: expected:\n%+v\n got missing key %+v", rt.name, port, rt.removes[port], port)
			} else {
				if len(rt.removes[port]) != len(removes[port]) {
					t.Errorf("Set %q: Removed port %s backends: expected:\n%+v\n got different lengths %d != %d", rt.name, port, rt.removes[port], len(rt.removes[port]), len(removes[port]))
				}
				for idx := range rt.removes[port] {
					if !rt.removes[port][idx].isEqual(removes[port][idx]) {
						t.Errorf("Set %q/%s: Removed port %s backends: expected:\n%+v\n got:\n%+v", rt.name, idx, port, rt.removes[port][idx], removes[port][idx])
					}
				}
			}
		}
		for port := range rt.upserts {
			if _, ok := upserts[port]; !ok {
				t.Errorf("Set %q: Upserted port %s backends: expected:\n%+v\n got missing key %+v", rt.name, port, rt.upserts[port], port)
			} else {
				if len(rt.upserts[port]) != len(upserts[port]) {
					t.Errorf("Set %q: Upserted port %s backends: expected:\n%+v\n got different lengths %d != %d", rt.name, port, rt.upserts[port], len(rt.upserts[port]), len(upserts[port]))
				} else {
					for idx := range rt.upserts[port] {
						if !rt.upserts[port][idx].isEqual(upserts[port][idx]) {
							t.Errorf("Set %q/%s: Upserted port %s backends: expected:\n%+v\n got:\n%+v", rt.name, idx, port, rt.upserts[port][idx], upserts[port][idx])
						}
					}
				}
			}
		}
	}
}

func Test_checkOperation(t *testing.T) {
	type test struct {
		noErr bool
		op    *compute.Operation
	}

	tests := []test{
		{
			noErr: false,
			op: &compute.Operation{
				Status: "invalid",
			},
		},
		{
			noErr: false,
			op: &compute.Operation{
				Status: computeOperationStatusPending,
			},
		},
		{
			noErr: false,
			op: &compute.Operation{
				Status: computeOperationStatusRunning,
			},
		},
		{
			noErr: false,
			op: &compute.Operation{
				Status: computeOperationStatusDone,
				Error:  &compute.OperationError{},
			},
		},
		{
			noErr: true,
			op: &compute.Operation{
				Status: computeOperationStatusDone,
			},
		},
	}
	for i, tt := range tests {
		err := checkOperation(tt.op)
		if (err == nil && !tt.noErr) || (err != nil && tt.noErr) {
			t.Errorf("%d: failed.", i+1)
		}
	}
}

func TestReconcileBackendsDeletionWithMissingBackend(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		t.Logf("Got request: %s", req.URL.String())
		// Return not found on backend service get.
		res.WriteHeader(http.StatusNotFound)
	}))
	cs, err := compute.NewService(context.Background(), option.WithEndpoint(s.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("Failed to instantiate compute service: %v", err)
	}
	bc := ProdBackendController{
		project: "test-project",
		s:       cs,
	}
	err = bc.ReconcileBackends(context.Background(), statusBasicWithNEGs, AutonegStatus{
		// On deletion, the intended state is set to empty.
		AutonegConfig: AutonegConfig{},
		NEGStatus:     negStatus,
	}, false)
	if err != nil {
		t.Errorf("ReconcileBackends() got err: %v, want none", err)
	}
}

func TestReconcileBackendsDeletionWithEmptyNEGStatus(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		t.Logf("Got request: %s", req.URL.String())
		// Return not found on backend service get.
		res.WriteHeader(http.StatusNotFound)
	}))
	cs, err := compute.NewService(context.Background(), option.WithEndpoint(s.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("Failed to instantiate compute service: %v", err)
	}
	bc := ProdBackendController{
		project: "test-project",
		s:       cs,
	}
	err = bc.ReconcileBackends(context.Background(), AutonegStatus{
		AutonegConfig: AutonegConfig{
			BackendServices: map[string]map[string]AutonegNEGConfig{
				"80": {
					"test": AutonegNEGConfig{
						Name: "test",
						Rate: 100,
					},
				},
			},
		},
		NEGStatus: NEGStatus{}, // NEG status not populated by GKE NEG controller.
	}, AutonegStatus{
		AutonegConfig: AutonegConfig{
			BackendServices: map[string]map[string]AutonegNEGConfig{},
		},
		NEGStatus: NEGStatus{},
	}, false)
	if err != nil {
		t.Errorf("ReconcileBackends() got err: %v, want none", err)
	}
}

type fakeBackendServiceHandler struct {
	sync.RWMutex
	bs             *compute.BackendService
	t              *testing.T
	expectedCalls  [][2]string
	operations     map[string]bool
	firstOpPending bool
}

func (h *fakeBackendServiceHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.t.Logf("Got request: %s %s %s", req.URL.Scheme, req.Method, req.URL.String())

	parts := strings.Split(req.URL.Path, "/")
	bsName := parts[len(parts)-1]
	resType := parts[len(parts)-2]

	h.t.Logf("Got backend service name: %s - resource type: %s", bsName, resType)

	if h.expectedCalls != nil {
		// check and fails if it is not as expected
		expectedCall := h.expectedCalls[0]
		if req.Method != expectedCall[0] || resType != expectedCall[1] {
			h.t.Fatalf("unexpected API call: expected: %v, got %v", expectedCall, [2]string{req.Method, resType})
			return
		}
		// pop the current one, expect the next
		h.expectedCalls = h.expectedCalls[1:]
	}

	switch req.Method {
	case http.MethodGet:
		if bsName == h.bs.Name {
			if resType == "operations" {
				opStatus := computeOperationStatusDone
				if h.firstOpPending {
					if h.operations == nil {
						h.operations = make(map[string]bool)
					}
					if !h.operations[bsName] {
						opStatus = computeOperationStatusPending
						h.operations[bsName] = true
					}
				}
				json.NewEncoder(w).Encode(compute.Operation{Status: opStatus})
				return
			}
			h.RLock()
			defer h.RUnlock()
			enc := json.NewEncoder(w)
			if err := enc.Encode(h.bs); err != nil {
				h.t.Fatalf("json encode failed: %v", err)
			}
			return
		}

	case http.MethodPatch:
		defer req.Body.Close()
		if bsName == h.bs.Name {
			patchBody := compute.BackendService{}
			dec := json.NewDecoder(req.Body)
			if err := dec.Decode(&patchBody); err != nil {
				h.t.Fatalf("json decode failed: %v", err)
			}
			h.Lock()
			defer h.Unlock()
			enc := json.NewEncoder(w)
			if err := enc.Encode(h.bs); err != nil {
				h.t.Fatalf("json encode failed: %v", err)
			}
			return
		}

	default:
		h.t.Fatalf("unexpected %s api call", req.Method)
	}
	w.WriteHeader(http.StatusNotFound)
}

func TestReconcileBackendsWithCustomMetricsAgainstFakeServer(t *testing.T) {
	project := "test-project"
	negStatusOneZone := NEGStatus{
		NEGs:  map[string]string{"80": "fake_neg"},
		Zones: []string{"zone1", "zone2"},
	}
	as := AutonegStatus{
		AutonegConfig: AutonegConfig{
			BackendServices: map[string]map[string]AutonegNEGConfig{
				"80": {
					"fake": AutonegNEGConfig{Name: "fake", Connections: 100},
				},
			},
		},
		NEGStatus: negStatusOneZone, // NEG status not populated by GKE NEG controller.
	}
	ab := as.Backend("fake", "80", getGroup(project, negStatusOneZone.Zones[0], negStatusOneZone.NEGs["80"]))

	is := AutonegStatus{
		AutonegConfig: AutonegConfig{
			BackendServices: map[string]map[string]AutonegNEGConfig{
				"80": {
					"fake": AutonegNEGConfig{
						Name: "fake",
						CustomMetrics: []AutonegCustomMetric{
							{Name: "orca.named_metrics.cool_one", MaxUtilization: 0.8},
						},
					},
				},
			},
		},
		NEGStatus: negStatusOneZone, // NEG status not populated by GKE NEG controller.
	}

	checkExpectedCallsAreDone := true
	fbsh := &fakeBackendServiceHandler{
		bs: &compute.BackendService{
			Kind:            "compute#backendService",
			Id:              1,
			Name:            "fake",
			ForceSendFields: []string{"Backends"},
			Backends:        []*compute.Backend{&ab},
		},
		t:              t,
		expectedCalls:  [][2]string{{"GET", "backendServices"}, {"PATCH", "backendServices"}, {"GET", "operations"}, {"GET", "operations"}},
		firstOpPending: true,
	}

	s := httptest.NewServer(fbsh)

	cs, err := compute.NewService(t.Context(), option.WithEndpoint(s.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("Failed to instantiate compute service: %v", err)
	}
	bc := ProdBackendController{
		project: project,
		s:       cs,
	}

	err = bc.ReconcileBackends(context.Background(), as, is, false)
	if err != nil {
		t.Errorf("ReconcileBackends() got err: %v, want none", err)
	}

	if checkExpectedCallsAreDone {
		if len(fbsh.expectedCalls) != 0 {
			t.Fatalf("Some expected calls not done, remaining uncalled: %v", fbsh.expectedCalls)
		}
	}
}

type fakeReader struct {
	client.Reader
	svcNeg *v1beta1.ServiceNetworkEndpointGroup
	getErr error
}

func (r *fakeReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if r.svcNeg != nil {
		r.svcNeg.DeepCopyInto(obj.(*v1beta1.ServiceNetworkEndpointGroup))
	}
	return r.getErr
}

func TestZonesFromSvcNeg(t *testing.T) {
	tests := []struct {
		name         string
		negStatus    *NEGStatus
		svcNeg       *v1beta1.ServiceNetworkEndpointGroup
		getSvcNegErr error
		wantZones    []string
		wantErr      bool
	}{
		{
			name: "success",
			svcNeg: &v1beta1.ServiceNetworkEndpointGroup{
				Status: v1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []v1beta1.NegObjectReference{
						{
							SelfLink: "https://www.googleapis.com/compute/beta/projects/test-project/zones/zone1/networkEndpointGroups/neg_name",
						},
						{
							SelfLink: "https://www.googleapis.com/compute/beta/projects/test-project/zones/zone2/networkEndpointGroups/neg_name",
						},
					},
				},
			},
			negStatus: &NEGStatus{
				NEGs: map[string]string{"80": fakeNeg, "90": fakeNeg2},
			},
			wantZones: []string{"zone1", "zone2"},
			wantErr:   false,
		},
		{
			name:         "svcneg not found",
			getSvcNegErr: apierrors.NewNotFound(schema.GroupResource{}, ""),
			negStatus: &NEGStatus{
				NEGs: map[string]string{"80": fakeNeg},
			},
			wantZones: []string{},
			wantErr:   false,
		},
		{
			name:         "get svcneg error",
			getSvcNegErr: apierrors.NewForbidden(schema.GroupResource{}, "", nil),
			negStatus: &NEGStatus{
				NEGs: map[string]string{"80": fakeNeg},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeReader{
				svcNeg: tt.svcNeg,
				getErr: tt.getSvcNegErr,
			}
			zones, err := zonesFromSvcNeg(context.Background(), r, "test", tt.negStatus)
			if (err != nil) != tt.wantErr {
				t.Errorf("ZonesFromSvcNeg() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(zones, tt.wantZones) {
				t.Errorf("ZonesFromSvcNeg() zones = %v, want %v", zones, tt.wantZones)
			}
		})
	}
}
