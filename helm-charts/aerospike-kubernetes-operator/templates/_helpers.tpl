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

`certs.webhook.provider` wins whenever it is set. `certs.webhook.create` is only
consulted as a legacy fallback, and an explicit false is NOT an error — in 4.5.0 it meant
"I create the Certificate named aerospike-operator-serving-cert myself, cainjector still
fills in the caBundle", not "no cert-manager". That maps onto none of the three
user-facing providers, so it resolves to the internal `legacyCertManager` state, which
renders exactly what 4.5.0 did. `legacyCertManager` is not a valid `provider` value and
cannot be selected deliberately.

Only a literal boolean false selects the legacy state. An absent or null `create` is not
the 4.5.0 opt-out (4.5.0's schema required the key, so that input could not exist) and
must fall through to the default, or a dropped key would silently produce an install with
no Certificate and no Secret.
*/}}
{{- define "aerospike-kubernetes-operator.webhookCertProvider" -}}
{{- $webhook := .Values.certs.webhook -}}
{{- if $webhook.provider -}}
{{- $webhook.provider -}}
{{- else if and (kindIs "bool" $webhook.create) (not $webhook.create) -}}
legacyCertManager
{{- else -}}
certManager
{{- end -}}
{{- end -}}

{{/*
Create the cert-manager Issuer/Certificate for the webhook. Only the explicit
certManager provider does; legacyCertManager deliberately does not.
*/}}
{{- define "aerospike-kubernetes-operator.webhookCreateCertManagerResources" -}}
{{- if eq (include "aerospike-kubernetes-operator.webhookCertProvider" .) "certManager" -}}true{{- end -}}
{{- end -}}

{{/*
Base64-encoded CA bundle to inline into each webhook clientConfig. Empty for the two
cert-manager states, where cainjector owns the field — so an empty result is also what
tells the webhook templates to emit the inject-ca-from annotation, which keeps "annotation
XOR inlined caBundle" true by construction rather than by a second, separate predicate.

Whitespace is stripped from the external bundle: the value must reach the API server as
single-line base64 (it decodes with strict base64), but `--set-file` from a file produced
by `base64` carries a trailing newline, and GNU base64 wraps at 76 columns by default.
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
The self-signed CA and serving certificate for provider=selfSigned, as base64.

Sprig's genSignedCert produces new material on every call, so the result is memoized into
.Values (safe: schemaRoot.additionalProperties is true, and JSON schema validation runs
before rendering). Helm's template render order is NOT deterministic, so every consumer
must reach the material THROUGH this helper — reading the cached key directly races the
template that populates it.

The values are kept base64-encoded because that is the form both consumers need (Secret
data and caBundle) and because the toYaml/fromYaml handoff below is not byte-faithful for
multi-line strings: Helm's toYaml trims the document's trailing newline, which silently
truncated the last-sorted PEM field by one byte when this cached decoded PEM.

An existing Secret is reused so `helm upgrade` does not rotate the certificate: the pod
picks up a remounted Secret on kubelet's schedule (up to ~60s) while the API server sees
the new caBundle at once, and with failurePolicy: Fail that gap is an admission outage.
All three keys must be present to reuse — a Secret pre-created for provider=external
legitimately has no ca.crt, and reusing it would silently yield an empty caBundle.

Deliberately no Secret-ownership check here. Switching an existing release to selfSigned
makes the chart the owner of a Secret that cert-manager created, and Helm refuses to adopt
a resource it does not own unless the user passes --take-ownership. A template cannot see
that flag (.Release exposes only IsInstall/IsUpgrade/Name/Namespace/Revision/Service), so
any check here would either be wrong or would have to be waived by a values key: it would
abort at render time, before Helm's own ownership validation, and so block the very
--take-ownership path that migrates without an admission outage. Helm's error already names
the Secret and every missing marker; the part it cannot know — that deleting the Secret
alone lets cert-manager re-issue it — is documented in the README instead.

Note `lookup` returns empty under `helm template` and `--dry-run`, so a render-and-apply
GitOps pipeline regenerates the certificate every time. Use certManager or external there.
*/}}
{{- define "aerospike-kubernetes-operator.webhookSelfSignedCert" -}}
{{- $cached := .Values._akoWebhookSelfSignedCert -}}
{{- if not $cached -}}
  {{- $name := .Values.certs.webhook.webhookServerCertSecretName -}}
  {{- $secret := lookup "v1" "Secret" .Release.Namespace $name -}}
  {{- $crt := dig "data" "tls.crt" "" $secret -}}
  {{- $key := dig "data" "tls.key" "" $secret -}}
  {{- $ca := dig "data" "ca.crt" "" $secret -}}
  {{- if and $crt $key $ca -}}
    {{- $cached = dict "crt" $crt "key" $key "ca" $ca "reused" true -}}
  {{- else -}}
    {{/* 10 years: nothing renews a chart-generated cert, and the mode is dev/POC only.
         NotBefore is the render machine's clock; see the clock-skew note in the README. */}}
    {{- $svc := include "aerospike-kubernetes-operator.webhookServiceName" . -}}
    {{- $dnsNames := include "aerospike-kubernetes-operator.webhookDnsNames" . | fromYamlArray -}}
    {{- $rootCa := genCA (printf "%s-ca" $svc) 3650 -}}
    {{- $cert := genSignedCert $svc nil $dnsNames 3650 $rootCa -}}
    {{- $cached = dict "crt" ($cert.Cert | b64enc) "key" ($cert.Key | b64enc) "ca" ($rootCa.Cert | b64enc) "reused" false -}}
  {{- end -}}
  {{- $_ := set .Values "_akoWebhookSelfSignedCert" $cached -}}
{{- end -}}
{{- toYaml $cached -}}
{{- end -}}
