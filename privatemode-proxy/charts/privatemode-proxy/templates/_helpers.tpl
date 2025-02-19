{{- define "privatemode-proxy.fullname" -}}
{{- include "privatemode-proxy.name" . }}-{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "privatemode-proxy.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "privatemode-proxy.selectorLabels" -}}
{{- include "privatemode-proxy.labels" . }}
{{- end }}

{{- define "privatemode-proxy.labels" -}}
helm.sh/chart: {{ include "privatemode-proxy.chart" . }}
app.kubernetes.io/name: {{ include "privatemode-proxy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}-{{ .Release.Namespace }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "privatemode-proxy.chart" -}}
{{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}
