#!/bin/bash

version="$1"
if [ -z "$version" ] ; then
    echo "Specify version to release!"
    exit 1
fi

SED=sed
if [ "$(uname -s)" == "Darwin" ] ; then
    # Install gnu-sed from Homebrew
    SED=gsed
fi

echo "Releasing version: $version"

$SED -i -e "s/google-pso-tool\/gke-autoneg-controller\/[^\"]*\"/google-pso-tool\/gke-autoneg-controller\/$version\"/" main.go
$SED -i -e "s/ghcr.io\/googlecloudplatform\/gke-autoneg-controller\/gke-autoneg-controller:[^\"]*\"/ghcr.io\/googlecloudplatform\/gke-autoneg-controller\/gke-autoneg-controller:v$version\"/" terraform/autoneg/variables.tf
$SED -i -e "s/ghcr.io\/googlecloudplatform\/gke-autoneg-controller\/gke-autoneg-controller:v.*$/ghcr.io\/googlecloudplatform\/gke-autoneg-controller\/gke-autoneg-controller:v$version/" deploy/autoneg.yaml
$SED -i -e "s/ghcr.io\/googlecloudplatform\/gke-autoneg-controller\/gke-autoneg-controller:v.*$/ghcr.io\/googlecloudplatform\/gke-autoneg-controller\/gke-autoneg-controller:v$version/" deploy/autoneg-namespaced.yaml

IFS='.' read -r -a newversion <<< "$version"

CHARTVERSION=$(grep '^version' deploy/chart/Chart.yaml | sed 's/version: //')
IFS='.' read -r -a va <<< "$CHARTVERSION"
CHARTPATCHVERSION=$((${va[2]}+1))
NEWCHARTVERSION="${va[0]}.${va[1]}.${CHARTPATCHVERSION}"
$SED -i -e "s/version: .*$/version: $NEWCHARTVERSION/" deploy/chart/Chart.yaml

$SED -i -e "s/appVersion: .*$/appVersion: $version/" deploy/chart/Chart.yaml

echo "Creating Git tag: v${version}"
git tag "v${version}"