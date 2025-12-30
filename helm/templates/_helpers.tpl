{{/*
Expand the name of the chart.
*/}}
{{- define "noccounting.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "noccounting.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "noccounting.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "noccounting.labels" -}}
helm.sh/chart: {{ include "noccounting.chart" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Bot selector labels
*/}}
{{- define "noccounting.bot.selectorLabels" -}}
app.kubernetes.io/name: {{ include "noccounting.name" . }}-bot
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: bot
{{- end }}

{{/*
Webapp selector labels
*/}}
{{- define "noccounting.webapp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "noccounting.name" . }}-webapp
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: webapp
{{- end }}

{{/*
Secret name
*/}}
{{- define "noccounting.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- include "noccounting.fullname" . }}-secret
{{- end }}
{{- end }}

{{/*
Webapp URL for bot
*/}}
{{- define "noccounting.webappURL" -}}
{{- if .Values.ingress.enabled }}
{{- printf "https://%s" .Values.ingress.hostname }}
{{- else }}
{{- "" }}
{{- end }}
{{- end }}
