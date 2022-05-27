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
	"reflect"
	"testing"

	"google.golang.org/api/compute/v1"
)

var (
	malformedJSON     = `{`
	validConfig       = `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}],"443":[{"name":"https-be","max_connections_per_endpoint":1000}]}}`
	brokenConfig      = `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":"100"}],"443":[{"name":"https-be","max_connections_per_endpoint":1000}}}`
	validMultiConfig  = `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100},{"name":"http-ilb-be","max_rate_per_endpoint":100}],"443":[{"name":"https-be","max_connections_per_endpoint":1000},{"name":"https-ilb-be","max_connections_per_endpoint":1000}]}}`
	validConfigWoName = `{"backend_services":{"80":[{"max_rate_per_endpoint":100}],"443":[{"max_connections_per_endpoint":1000}]}}`

	validStatus        = `{}`
	validAutonegConfig = `{}`
	validAutonegStatus = `{}`
	invalidStatus      = `{`
	oldValidConfig     = `{"name":"test", "max_rate_per_endpoint":100}`
	oldBrokenConfig    = `{"name":"test", "max_rate_per_endpoint":"100"}`

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
}

var oldStatusTests = []struct {
	name        string
	annotations map[string]string
	valid       bool
	err         bool
}{
	{
		"(legacy) not using autoneg",
		map[string]string{},
		false,
		false,
	},
	{
		"(legacy) autoneg with malformed config",
		map[string]string{
			oldAutonegAnnotation: malformedJSON,
			negAnnotation:        validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) autoneg with broken config",
		map[string]string{
			oldAutonegAnnotation: oldBrokenConfig,
			negAnnotation:        validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negAnnotation:        validNegConfig,
		},
		true,
		false,
	},
	{
		"(legacy) valid autoneg with too many ports",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negAnnotation:        tooManyNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg with invalid status",
		map[string]string{
			oldAutonegAnnotation:       oldValidConfig,
			oldAutonegStatusAnnotation: malformedJSON,
			negAnnotation:              validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg with valid status",
		map[string]string{
			oldAutonegAnnotation:       oldValidConfig,
			oldAutonegStatusAnnotation: validStatus,
			negAnnotation:              validNegConfig,
		},
		true,
		false,
	},
	{
		"(legacy) valid autoneg with invalid neg status",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negStatusAnnotation:  malformedJSON,
			negAnnotation:        validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg with valid neg status",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negStatusAnnotation:  validAutonegStatus,
			negAnnotation:        validNegConfig,
		},
		true,
		false,
	},
}

func TestGetStatuses(t *testing.T) {
	for _, st := range statusTests {
		_, valid, err := getStatuses("ns", "test", st.annotations, "{namespace}-{name}-{port}-{hash}", true)
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

func TestGetOldStatuses(t *testing.T) {
	for _, st := range oldStatusTests {
		_, valid, err := getStatuses("ns", "test", st.annotations, "{namespace}-{name}-{port}-{hash}", true)
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
	validConf := `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}]}}`
	statuses, valid, err := getStatuses("ns", "test", map[string]string{autonegAnnotation: validConf}, "{namespace}-{name}-{port}", false)
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
	validConf := `{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}]}}`
	statuses, valid, err := getStatuses("ns", "test", map[string]string{autonegAnnotation: validConf}, "{namespace}-{name}-{port}", true)
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

var configTests = []struct {
	name   string
	config OldAutonegConfig
	err    bool
}{
	{
		"default config",
		OldAutonegConfig{},
		false,
	},
}

func TestValidateConfig(t *testing.T) {
	for _, ct := range configTests {
		err := validateOldConfig(ct.config)
		if err == nil && ct.err {
			t.Errorf("Set %q: expected error, got none", ct.name)
		}
		if err != nil && !ct.err {
			t.Errorf("Set %q: expected no error, got one: %v", ct.name, err)
		}
	}
}

func relevantCopy(a compute.Backend) compute.Backend {
	b := compute.Backend{}
	b.Group = a.Group
	b.MaxRatePerEndpoint = a.MaxRatePerEndpoint
	b.MaxConnectionsPerEndpoint = a.MaxConnectionsPerEndpoint
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

	statusBasicWithNEGs = AutonegStatus{
		AutonegConfig: configBasic,
		NEGStatus:     negStatus,
	}
	backendsBasicWithNEGs = map[string]map[string]Backends{"80": map[string]Backends{"test": Backends{name: "test", backends: []compute.Backend{
		statusBasicWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusBasicWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone2", fakeNeg)),
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
	backendsValueChangeWithNEGs = map[string]map[string]Backends{"80": map[string]Backends{"test": Backends{name: "test", backends: []compute.Backend{
		statusValueChangeWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusValueChangeWithNEGs.Backend("test", "80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}}

	// named changed state
	configNameChangePort80        = AutonegNEGConfig{Name: "changed", Rate: 100}
	configNameChangePort80Slice   = map[string]AutonegNEGConfig{"changed": configValueChangePort80}
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

	// {
	// 	"basic to name changed",
	// 	statusBasicWithNEGs,
	// 	statusNameChangeWithNEGs,
	// 	backendsBasicWithNEGs,
	// 	backendsNameChangeWithNEGs,
	// },
}

func TestReconcileStatuses(t *testing.T) {
	for _, rt := range reconcileTests {
		removes, upserts := ReconcileStatus(fakeProject, rt.actual, rt.intended)
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
