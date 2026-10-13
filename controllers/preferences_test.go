package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcilePreferredRegion(t *testing.T) {
	for _, tt := range []struct {
		name   string
		region string
		want   string
	}{
		{"preferred region", "europe-west1", "PREFERRED"},
		{"other region", "us-central1", "DEFAULT"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			annotation := `{"backend_services":{"80":[{"name":"automatic","max_rate_per_endpoint":100,"preferred_region":"europe-west1"},{"name":"explicit-default","max_rate_per_endpoint":100,"preference":"DEFAULT","preferred_region":"europe-west1"}],"443":[{"name":"explicit-preferred","max_connections_per_endpoint":100,"preference":"PREFERRED","preferred_region":"europe-west1"},{"name":"automatic-tls","max_connections_per_endpoint":100,"preferred_region":"europe-west1"}]}}`
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name: "checkout", Namespace: "payments",
				Annotations: map[string]string{autonegAnnotation: annotation},
			}}
			r := &ServiceReconciler{
				Client:            fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(svc).Build(),
				BackendController: stubBackendController{}, Recorder: record.NewFakeRecorder(10),
				AllowServiceName: true, ErrorCount: map[string]int{}, localRegion: tt.region,
			}
			key := types.NamespacedName{Namespace: "payments", Name: "checkout"}
			if _, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: key}); err != nil {
				t.Fatal(err)
			}
			if err := r.Get(t.Context(), key, svc); err != nil {
				t.Fatal(err)
			}
			if svc.Annotations[autonegAnnotation] != annotation {
				t.Fatal("input annotation changed")
			}
			var status AutonegStatus
			if err := json.Unmarshal([]byte(svc.Annotations[autonegStatusAnnotation]), &status); err != nil {
				t.Fatal(err)
			}
			for _, expected := range []struct{ port, backend, preference string }{
				{"80", "automatic", tt.want}, {"443", "automatic-tls", tt.want},
				{"80", "explicit-default", "DEFAULT"}, {"443", "explicit-preferred", "PREFERRED"},
			} {
				if got := status.BackendServices[expected.port][expected.backend].Preference; got != expected.preference {
					t.Errorf("%s preference = %q; want %q", expected.backend, got, expected.preference)
				}
			}
		})
	}
}

func TestPreferredRegionLookup(t *testing.T) {
	lookupCount := 0
	r := &ServiceReconciler{AllowServiceName: true, regionResolver: func(context.Context) (string, error) {
		lookupCount++
		if lookupCount == 1 {
			return "", errors.New("metadata unavailable")
		}
		return "us-central1", nil
	}}
	annotation := map[string]string{autonegAnnotation: `{"backend_services":{"80":[{"name":"backend","max_rate_per_endpoint":100,"preferred_region":"us-central1"}]}}`}
	if _, _, err := getStatuses(t.Context(), "ns", "svc", annotation, r); err == nil {
		t.Fatal("metadata failure must fail reconciliation")
	}
	for range 2 {
		statuses, _, err := getStatuses(t.Context(), "ns", "svc", annotation, r)
		if err != nil {
			t.Fatal(err)
		}
		if got := statuses.config.BackendServices["80"]["backend"].Preference; got != "PREFERRED" {
			t.Fatalf("preference = %q; want PREFERRED", got)
		}
	}
	if lookupCount != 2 {
		t.Fatalf("metadata lookup count = %d; want 2", lookupCount)
	}
	for _, input := range []string{
		`{"backend_services":{"80":[{"max_rate_per_endpoint":100}]}}`,
		`{"backend_services":{"80":[{"max_rate_per_endpoint":100,"preference":"DEFAULT","preferred_region":"us-central1"}]}}`,
	} {
		fresh := &ServiceReconciler{ServiceNameTemplate: "{name}-{port}", regionResolver: func(context.Context) (string, error) {
			t.Fatal("metadata lookup should not be needed")
			return "", nil
		}}
		if _, _, err := getStatuses(t.Context(), "ns", "svc", map[string]string{autonegAnnotation: input}, fresh); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := getStatuses(t.Context(), "ns", "svc", map[string]string{
		autonegAnnotation: `{"backend_services":{"80":[{"max_rate_per_endpoint":100,"preferred_region":"us-central1-a"}]}}`,
	}, &ServiceReconciler{regionResolver: func(context.Context) (string, error) {
		t.Fatal("invalid region should be rejected before metadata lookup")
		return "", nil
	}}); err == nil {
		t.Fatal("zone must not be accepted as preferred_region")
	}
}

func TestRegionFromZone(t *testing.T) {
	for _, tt := range []struct {
		zone string
		want string
	}{
		{"europe-west1-b", "europe-west1"},
		{"us-central1-a", "us-central1"},
		{"northamerica-northeast2-c", "northamerica-northeast2"},
		{"projects/123/zones/asia-southeast1-a", "asia-southeast1"},
		{"", ""},
		{"europe-west1", ""},
		{"europe-west1-", ""},
		{"europe-west1-ab", ""},
		{"europe-west1-1", ""},
		{"invalid-a", ""},
	} {
		t.Run(tt.zone, func(t *testing.T) {
			got, err := regionFromZone(tt.zone)
			if got != tt.want || (err != nil) != (tt.want == "") {
				t.Fatalf("regionFromZone(%q) = %q, %v; want %q", tt.zone, got, err, tt.want)
			}
		})
	}
}

func TestReconcileBackendsPreference(t *testing.T) {
	for _, tt := range []struct{ name, old, intended, want string }{
		{"enable", "", "PREFERRED", "PREFERRED"},
		{"promote", "DEFAULT", "PREFERRED", "PREFERRED"},
		{"demote", "PREFERRED", "DEFAULT", "DEFAULT"},
		{"remove configuration", "PREFERRED", "", "DEFAULT"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			makeStatus := func(preference string) AutonegStatus {
				return AutonegStatus{
					AutonegConfig: AutonegConfig{BackendServices: map[string]map[string]AutonegNEGConfig{
						"80": {"fake": {Name: "fake", Rate: 100, Preference: preference}},
					}},
					NEGStatus: NEGStatus{NEGs: map[string]string{"80": "neg"}, Zones: []string{"us-central1-a"}},
				}
			}
			actual, intended := makeStatus(tt.old), makeStatus(tt.intended)
			backend := actual.Backend("fake", "80", getGroup("project", "us-central1-a", "neg"))
			other := &compute.Backend{Group: "another-cluster-neg", BalancingMode: "RATE", MaxRatePerEndpoint: 200, CapacityScaler: 1, Preference: "PREFERRED"}
			server := newFakeBackendServiceServer(
				map[string]*compute.BackendService{"fake": {Name: "fake", Backends: []*compute.Backend{&backend, other}}},
				map[string][][2]string{"fake": {{"GET", "backendServices"}, {"PATCH", "backendServices"}, {"GET", "operations"}}},
				map[string][]string{"fake": {computeOperationStatusDone}}, t,
			)
			t.Cleanup(server.Close)
			service, err := compute.NewService(t.Context(), option.WithEndpoint(server.URL), option.WithoutAuthentication())
			if err != nil {
				t.Fatal(err)
			}
			controller := NewBackendController("project", service)
			if err := controller.ReconcileBackends(context.Background(), actual, intended, false); err != nil {
				t.Fatal(err)
			}
			if remaining := server.bsExpectedCalls["fake"]; len(remaining) != 0 {
				t.Fatalf("missing API calls: %v", remaining)
			}
			backends := server.bss["fake"].Backends
			if len(backends) != 2 || backends[0].Preference != tt.want || backends[1].Preference != other.Preference {
				t.Fatalf("unexpected backends after preference update: %+v", backends)
			}
			// The API can return DEFAULT for an omitted preference; that must not trigger another patch.
			server.bsExpectedCalls["fake"] = [][2]string{{"GET", "backendServices"}}
			if err := controller.ReconcileBackends(t.Context(), intended, intended, false); err != nil {
				t.Fatal(err)
			}
			if len(server.bsExpectedCalls["fake"]) != 0 {
				t.Fatal("second reconciliation did not read backend service")
			}
		})
	}
}
