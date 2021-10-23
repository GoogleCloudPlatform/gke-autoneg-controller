#!/bin/bash
# Copyright 2019 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

gcloud iam service-accounts create autoneg-system --display-name "autoneg"
gcloud iam service-accounts add-iam-policy-binding \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:${PROJECT_ID}.svc.id.goog[autoneg-system/autoneg]" \
  autoneg-system@${PROJECT_ID}.iam.gserviceaccount.com

gcloud iam roles create autoneg --project ${PROJECT_ID} \
  --permissions=compute.backendServices.get,compute.backendServices.update,compute.regionBackendServices.get,compute.regionBackendServices.update,compute.networkEndpointGroups.use,compute.healthChecks.useReadOnly,compute.regionHealthChecks.useReadOnly
gcloud projects add-iam-policy-binding \
  --role projects/${PROJECT_ID}/roles/autoneg \
  --member "serviceAccount:autoneg-system@${PROJECT_ID}.iam.gserviceaccount.com" \
  ${PROJECT_ID}
