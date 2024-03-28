variable "custom_role_add_random_suffix" {
  type        = bool
  description = "Sets random suffix at the end of the IAM custom role id"
  default     = false
}

variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "regional" {
  description = "Whether or not to add regionBackend and regionHealthCheck permissions."
  default     = true
  type        = bool
}

variable "service_account_id" {
  description = "Service account id to be created"
  default     = "autoneg"
  type        = string
}

variable "shared_vpc" {
  description = "Shared VPC configuration which the autoneg service account can use"
  default     = null
  type = object({
    project_id        = string
    subnetwork_region = string
    subnetwork_id     = string
  })
}

variable "workload_identity" {
  description = "Workload identity configuration"
  type = object({
    namespace       = string
    service_account = string
  })
  default = {
    namespace       = "autoneg-system"
    service_account = "autoneg"
  }
}
