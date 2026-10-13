package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadPreferredRegions(t *testing.T) {
	for _, tt := range []struct {
		name    string
		data    string
		wantErr bool
	}{
		{"valid", "preferredRegions:\n  payments:\n    checkout: europe-west1\n    refunds: us-central1\n  search:\n    query: europe-west4\n", false},
		{"empty", "preferredRegions: {}", false},
		{"unknown field", "preferredRegion: {}", true},
		{"duplicate service", "preferredRegions:\n  payments:\n    checkout: europe-west1\n    checkout: us-central1", true},
		{"invalid namespace", "preferredRegions:\n  Bad:\n    checkout: europe-west1", true},
		{"invalid service", "preferredRegions:\n  payments:\n    Bad: europe-west1", true},
		{"zone instead of region", "preferredRegions:\n  payments:\n    checkout: europe-west1-b", true},
		{"empty region", "preferredRegions:\n  payments:\n    checkout: ''", true},
		{"null region", "preferredRegions:\n  payments:\n    checkout: null", true},
		{"malformed YAML", "preferredRegions: [", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "preferences.yaml")
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := loadPreferredRegions(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadPreferredRegions() = %v, %v; want error %v", got, err, tt.wantErr)
			}
			if tt.name == "valid" {
				want := map[string]map[string]string{
					"payments": {"checkout": "europe-west1", "refunds": "us-central1"},
					"search":   {"query": "europe-west4"},
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %v; want %v", got, want)
				}
			}
		})
	}
	if got, err := loadPreferredRegions(""); err != nil || got != nil {
		t.Fatalf("disabled configuration = %v, %v", got, err)
	}
	if _, err := loadPreferredRegions(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing configuration file must fail")
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
