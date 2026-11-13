{{- define "turnbacks.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "turnbacks.fullname" -}}
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

{{- define "turnbacks.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "turnbacks.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: turnbacks
{{- end }}

{{- define "turnbacks.selectorLabels" -}}
app.kubernetes.io/name: {{ include "turnbacks.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "turnbacks.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "turnbacks.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* CloudNativePG cluster name; its app Secret is "<cluster>-app". */}}
{{- define "turnbacks.dbClusterName" -}}
{{- printf "%s-db" (include "turnbacks.fullname" .) | trunc 50 | trimSuffix "-" }}
{{- end }}

{{- define "turnbacks.dbSecretName" -}}
{{- if .Values.database.cloudnativepg.enabled }}
{{- printf "%s-app" (include "turnbacks.dbClusterName" .) }}
{{- else }}
{{- required "database.external.existingSecret is required when database.cloudnativepg.enabled=false" .Values.database.external.existingSecret }}
{{- end }}
{{- end }}

{{- define "turnbacks.dbSecretKey" -}}
{{- if .Values.database.cloudnativepg.enabled }}uri{{ else }}{{ .Values.database.external.key }}{{ end }}
{{- end }}

{{- define "turnbacks.image" -}}
{{- if .Values.image.digest }}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else }}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}
{{- end }}

{{- define "turnbacks.ingressHost" -}}
{{- if .Values.ingress.host }}{{ .Values.ingress.host }}{{ else }}{{ (urlParse (required "ingress.host or publicURL is required when ingress.enabled" .Values.publicURL)).host }}{{ end }}
{{- end }}
