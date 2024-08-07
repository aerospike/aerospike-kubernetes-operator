{{/*
Expand the name of the chart.
*/}}
{{- define "aerospike-backup-service.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aerospike-backup-service.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Aerospike Backup Service common name.
*/}}
{{- define "aerospike-backup-service.commonName" -}}
{{- if .Values.commonName -}}
{{- .Values.commonName -}}
{{- else -}}
{{- .Release.Name | trunc 63 | replace "-" "" -}}
{{- end -}}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "aerospike-backup-service.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aerospike-backup-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "aerospike-backup-service.labels" -}}
helm.sh/chart: {{ include "aerospike-backup-service.chart" . }}
{{ include "aerospike-backup-service.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}