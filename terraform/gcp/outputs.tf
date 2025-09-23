/**
 * Copyright 2021 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
output "service_account_email" {
  value = var.workload_identity != null ? google_service_account.autoneg[0].email : null
}

output "service_account" {
  value = var.workload_identity != null ? google_service_account.autoneg[0] : null
}

output "autoneg_custom_role" {
  value = google_project_iam_custom_role.autoneg
}
