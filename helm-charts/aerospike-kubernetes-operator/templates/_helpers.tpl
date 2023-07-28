{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "aerospike-kubernetes-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "aerospike-kubernetes-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aerospike-kubernetes-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "aerospike-kubernetes-operator.labels" -}}
helm.sh/chart: {{ include "aerospike-kubernetes-operator.chart" . }}
{{ include "aerospike-kubernetes-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "aerospike-kubernetes-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aerospike-kubernetes-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}


{{/*
Deprecated fields are not allowed to be used with new charts, install/upgrade should fail
*/}}
{{- define "validateDeprecateFields" -}}
{{- if .Values.kubeRBACProxyPort -}}
    {{ fail ".Values.kubeRBACProxyPort field is deprecated, use .Values.kubeRBACProxy.port instead" }}
{{- end -}}

{{- if .Values.webhookServicePort -}}
    {{ fail ".Values.webhookServicePort field is deprecated, use .Values.webhookService.targetPort instead" }}
{{- end -}}

{{- end -}}