variable "controller_image" {
  type        = string
  description = "Autoneg controller container image"
}

variable "extra_args" {
  type        = list(string)
  default     = []
  description = "Arguments added to the autoneg controller start"
}

variable "image_pull_policy" {
  type        = string
  description = "Image pull policy for Autoneg container"
  default     = "IfNotPresent"
}

variable "kube_rbac_proxy_image" {
  type        = string
  description = "kuber-rbac-proxy container image"
  default     = "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0"
}

variable "namespace" {
  type        = string
  description = "Autoneg namespace"
  default     = "autoneg-system"
}

variable "priority_class_name" {
  description = "Pod's PriorityClass name"
  type        = string
  default     = null
}

variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "service_account_email" {
  type        = string
  description = "Autoneg service account email"
}

variable "service_account_id" {
  type        = string
  description = "Autoneg service account"
  default     = "autoneg"
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
