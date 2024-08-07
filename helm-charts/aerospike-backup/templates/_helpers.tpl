{{/*
Expand the name of the chart.
*/}}
{{- define "aerospike-backup.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aerospike-backup.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Aerospike Backup Service common name.
*/}}
{{- define "aerospike-backup.commonName" -}}
{{- if .Values.commonName -}}
{{- .Values.commonName -}}
{{- else -}}
{{- .Release.Name | trunc 63 | replace "-" "" -}}
{{- end -}}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "aerospike-backup.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aerospike-backup.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "aerospike-backup.labels" -}}
helm.sh/chart: {{ include "aerospike-backup.chart" . }}
{{ include "aerospike-backup.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}