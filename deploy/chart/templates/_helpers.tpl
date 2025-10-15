{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "autoneg.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "autoneg.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Return the full image reference. Prefer digest if provided.
- If .Values.gke_autoneg_controller.image.digest is set: repo@digest
- Else if .Values.gke_autoneg_controller.image.tag is set: repo:tag
- Else: repo:v.Chart.AppVersion
Also: prevent using both tag and digest.
*/}}
{{- define "autoneg.image" -}}
{{- $repo := required "gke_autoneg_controller.image.repository is required" .Values.gke_autoneg_controller.image.repository -}}
{{- $tag := .Values.gke_autoneg_controller.image.tag | toString | trim -}}
{{- $digest := .Values.gke_autoneg_controller.image.digest | toString | trim -}}

{{- if and $tag $digest -}}
  {{- fail "Specify either gke_autoneg_controller.image.tag or gke_autoneg_controller.image.digest, not both." -}}
{{- end -}}

{{- if $digest -}}
  {{- if not (regexMatch `^sha256:[A-Fa-f0-9]{64}$` $digest) -}}
    {{- fail (printf "gke_autoneg_controller.image.digest must match ^sha256:[0-9a-f]{64}$, got %q" $digest) -}}
  {{- end -}}

  {{- $repo -}}@{{ $digest }}
{{- else if $tag -}}
  {{- $repo -}}:{{ $digest }}
{{- else -}}
  {{- $repo -}}:v{{ .Chart.AppVersion }}
{{- end -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "autoneg.labels" -}}
helm.sh/chart: {{ include "autoneg.chart" . }}
{{ include "autoneg.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.labels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Expand the name of the chart.
*/}}
{{- define "autoneg.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Allow the release namespace to be overridden for multi-namespace deployments in combined charts.
*/}}
{{- define "autoneg.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "autoneg.selectorLabels" -}}
app.kubernetes.io/name: {{ include "autoneg.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "autoneg.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "autoneg.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
<<<<<<< HEAD

