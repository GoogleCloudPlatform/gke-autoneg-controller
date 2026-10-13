package controllers

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
)

var regionPattern = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)+[0-9]+$`)

func regionFromZone(zone string) (string, error) {
	region := zone[strings.LastIndex(zone, "/")+1:]
	index := strings.LastIndex(region, "-")
	if index < 0 || len(region[index+1:]) != 1 || region[index+1] < 'a' || region[index+1] > 'z' || !regionPattern.MatchString(region[:index]) {
		return "", fmt.Errorf("invalid metadata zone %q", zone)
	}
	return region[:index], nil
}

func (r *ServiceReconciler) getLocalRegion(ctx context.Context) (string, error) {
	r.localRegionMu.Lock()
	defer r.localRegionMu.Unlock()
	if r.localRegion != "" {
		return r.localRegion, nil
	}
	resolve := r.regionResolver
	if resolve == nil {
		resolve = func(ctx context.Context) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			zone, err := metadata.ZoneWithContext(ctx)
			if err != nil {
				return "", err
			}
			return regionFromZone(zone)
		}
	}
	region, err := resolve(ctx)
	if err != nil {
		return "", err
	}
	if !regionPattern.MatchString(region) {
		return "", fmt.Errorf("invalid local region %q", region)
	}
	r.localRegion = region
	return region, nil
}
