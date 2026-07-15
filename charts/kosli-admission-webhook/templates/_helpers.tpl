{{/* Chart name */}}
{{- define "kosli-webhook.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Fully qualified name */}}
{{- define "kosli-webhook.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "kosli-webhook.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Common labels */}}
{{- define "kosli-webhook.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
app.kubernetes.io/name: {{ include "kosli-webhook.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* Selector labels */}}
{{- define "kosli-webhook.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kosli-webhook.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Service account name */}}
{{- define "kosli-webhook.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "kosli-webhook.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* Name of the secret holding the Kosli API token */}}
{{- define "kosli-webhook.tokenSecretName" -}}
{{- if .Values.kosli.existingSecret -}}
{{- .Values.kosli.existingSecret -}}
{{- else -}}
{{- printf "%s-credentials" (include "kosli-webhook.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/* Name of the TLS secret */}}
{{- define "kosli-webhook.tlsSecretName" -}}
{{- if eq .Values.certificates.provider "manual" -}}
{{- required "certificates.manual.secretName is required when certificates.provider=manual" .Values.certificates.manual.secretName -}}
{{- else -}}
{{- printf "%s-tls" (include "kosli-webhook.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/* Validate mutually exclusive assertion scopes */}}
{{- define "kosli-webhook.validateScope" -}}
{{- if and .Values.kosli.environment .Values.kosli.policyNames -}}
{{- fail "kosli.environment and kosli.policyNames are mutually exclusive" -}}
{{- end -}}
{{- if and (not .Values.kosli.environment) (not .Values.kosli.policyNames) -}}
{{- fail "one of kosli.environment or kosli.policyNames must be set" -}}
{{- end -}}
{{- end -}}

{{/* Validate certificates.provider */}}
{{- define "kosli-webhook.validateCertProvider" -}}
{{- $p := .Values.certificates.provider -}}
{{- if not (has $p (list "cert-manager" "openshift-service-ca" "manual")) -}}
{{- fail (printf "certificates.provider must be one of cert-manager, openshift-service-ca, manual (got %q)" $p) -}}
{{- end -}}
{{- end -}}
