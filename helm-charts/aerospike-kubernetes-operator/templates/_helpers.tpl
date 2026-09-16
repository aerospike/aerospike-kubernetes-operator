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

{{- if .Values.webhookServicePort -}}
    {{ fail ".Values.webhookServicePort field is deprecated, use .Values.webhookService.targetPort instead" }}
{{- end -}}

{{- end -}}

{{/*
Name of the webhook Service, and the two DNS names that resolve to it. Single source of
truth for the SANs: the cert-manager Certificate's dnsNames and the Sprig-generated
self-signed certificate must agree, or the API server rejects the TLS handshake.
*/}}
{{- define "aerospike-kubernetes-operator.webhookServiceName" -}}
aerospike-operator-webhook-service
{{- end -}}

{{- define "aerospike-kubernetes-operator.webhookDnsNames" -}}
{{- $svc := include "aerospike-kubernetes-operator.webhookServiceName" . -}}
{{- list (printf "%s.%s.svc" $svc .Release.Namespace) (printf "%s.%s.svc.cluster.local" $svc .Release.Namespace) | toYaml -}}
{{- end -}}

{{/*
Resolve the effective webhook certificate mode.

`provider` wins when set. `create` is the deprecated fallback, read with plain truthiness
exactly as 4.5.0 read it, so false, null and absent all land on `legacyCertManager`.

`legacyCertManager` is what 4.5.0 rendered for create=false: no Issuer or Certificate, but
still the inject-ca-from annotation, because the user supplies the Certificate themselves.
It is internal, not a selectable `provider` value — existing create=false releases leave
the key as it is; the README covers moving onto `provider`.
*/}}
{{- define "aerospike-kubernetes-operator.webhookCertProvider" -}}
{{- $webhook := .Values.certs.webhook -}}
{{- if $webhook.provider -}}
{{- $webhook.provider -}}
{{- else if not $webhook.create -}}
legacyCertManager
{{- else -}}
certManager
{{- end -}}
{{- end -}}

{{/*
Create the cert-manager Issuer/Certificate for the webhook. Only the explicit
certManager provider or deprecated create: true enables this; legacyCertManager deliberately does not.
*/}}
{{- define "aerospike-kubernetes-operator.webhookCreateCertManagerResources" -}}
{{- if eq (include "aerospike-kubernetes-operator.webhookCertProvider" .) "certManager" -}}true{{- end -}}
{{- end -}}

{{/*
Base64 CA bundle for the webhook clientConfig.

Empty under either cert-manager mode, where cainjector fills caBundle in and the webhook
templates emit the inject-ca-from annotation instead. External bundles have whitespace
stripped; the API server needs single-line base64.
*/}}
{{- define "aerospike-kubernetes-operator.webhookCaBundle" -}}
{{- $provider := include "aerospike-kubernetes-operator.webhookCertProvider" . -}}
{{- if eq $provider "selfSigned" -}}
{{- (include "aerospike-kubernetes-operator.webhookSelfSignedCert" . | fromYaml).ca -}}
{{- else if eq $provider "external" -}}
{{- .Values.certs.webhook.external.caBundle | nospace -}}
{{- end -}}
{{- end -}}

{{/*
CA and serving certificate for provider=selfSigned, base64-encoded.

genSignedCert returns new material on every call, so the result is memoized into .Values
(the schema root allows additional properties, and validation runs before rendering).
Every consumer must read it through this helper rather than the cached key directly, so
the value never depends on which template renders first.

Kept base64 because that is the form both consumers need, and because toYaml/fromYaml is
not byte-faithful for multi-line strings: toYaml drops the trailing newline of the
last-sorted field, truncating a PEM by one byte.

An existing Secret is reused only when it passes two checks, otherwise it is regenerated:

  1. Ownership — its meta.helm.sh/release-name annotation equals .Release.Name. Helm sets
     that annotation on resources it manages, so a Secret issued by cert-manager or by
     another release fails this and is never adopted.
  2. Completeness — tls.crt, tls.key and ca.crt are all present. A Secret pre-created for
     provider=external has no ca.crt, and reusing it would yield an empty caBundle.

Reuse is what stops `helm upgrade` rotating the certificate: the pod remounts on kubelet's
schedule while the API server sees the new caBundle at once, and with failurePolicy: Fail
that gap is an admission outage.

`lookup` is empty under `helm template` and `--dry-run`, so a render-and-apply pipeline
regenerates every time. Use certManager or external there.
*/}}
{{- define "aerospike-kubernetes-operator.webhookSelfSignedCert" -}}
{{- $cached := .Values._akoWebhookSelfSignedCert -}}
{{- if not $cached -}}
  {{- $name := .Values.certs.webhook.webhookServerCertSecretName -}}
  {{- $secret := lookup "v1" "Secret" .Release.Namespace $name -}}
  {{- $crt := dig "data" "tls.crt" "" $secret -}}
  {{- $key := dig "data" "tls.key" "" $secret -}}
  {{- $ca := dig "data" "ca.crt" "" $secret -}}
  {{- $owner := dig "metadata" "annotations" "meta.helm.sh/release-name" "" $secret -}}
  {{- if and $crt $key $ca (eq $owner .Release.Name) -}}
    {{- $cached = dict "crt" $crt "key" $key "ca" $ca "reused" true -}}
  {{- else -}}
    {{/* 1 year. Nothing renews a chart-generated cert, so the release has to be upgraded
         with the Secret deleted before it expires. NotBefore is the render machine's
         clock; see the clock-skew note in the README. */}}
    {{- $svc := include "aerospike-kubernetes-operator.webhookServiceName" . -}}
    {{- $dnsNames := include "aerospike-kubernetes-operator.webhookDnsNames" . | fromYamlArray -}}
    {{- $rootCa := genCA (printf "%s-ca" $svc) 365 -}}
    {{- $cert := genSignedCert $svc nil $dnsNames 365 $rootCa -}}
    {{- $cached = dict "crt" ($cert.Cert | b64enc) "key" ($cert.Key | b64enc) "ca" ($rootCa.Cert | b64enc) "reused" false -}}
  {{- end -}}
  {{- $_ := set .Values "_akoWebhookSelfSignedCert" $cached -}}
{{- end -}}
{{- toYaml $cached -}}
{{- end -}}
