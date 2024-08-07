{{/*
Expand the name of the chart.
*/}}
{{- define "aerospike-restore.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aerospike-restore.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Aerospike Restore Service common name.
*/}}
{{- define "aerospike-restore.commonName" -}}
{{- if .Values.commonName -}}
{{- .Values.commonName -}}
{{- else -}}
{{- .Release.Name | trunc 63 | replace "-" "" -}}
{{- end -}}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "aerospike-restore.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aerospike-restore.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "aerospike-restore.labels" -}}
helm.sh/chart: {{ include "aerospike-restore.chart" . }}
{{ include "aerospike-restore.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}