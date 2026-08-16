{{/*
Expand the name of the chart.
*/}}
{{- define "tilegroxy.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "tilegroxy.fullname" -}}
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

{{- define "tilegroxy.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "tilegroxy.labels" -}}
helm.sh/chart: {{ include "tilegroxy.chart" . }}
{{ include "tilegroxy.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "tilegroxy.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tilegroxy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "tilegroxy.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "tilegroxy.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the ConfigMap holding tilegroxy.yml. When existingConfigMap is set the
chart renders no ConfigMap of its own and mounts that one instead.
*/}}
{{- define "tilegroxy.configMapName" -}}
{{- default (printf "%s-config" (include "tilegroxy.fullname" .)) .Values.existingConfigMap }}
{{- end }}

{{- define "tilegroxy.secretName" -}}
{{- printf "%s-env" (include "tilegroxy.fullname" .) }}
{{- end }}

{{- define "tilegroxy.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end }}

{{/*
The config, guaranteed to be a dict so `dig` below is safe even when the user
sets `config: null`. Config keys are case-insensitive in tilegroxy itself, but
the lookups here are not, so the documented casing is what the chart reads.
*/}}
{{- define "tilegroxy.config" -}}
{{- default dict .Values.config | toYaml -}}
{{- end }}

{{- define "tilegroxy.server" -}}
{{- dig "server" dict (default dict .Values.config) | toYaml -}}
{{- end }}

{{/*
The port tilegroxy serves tiles on. Taken from the config so the Service,
probes and container ports stay consistent with whatever the user configured.
*/}}
{{- define "tilegroxy.serverPort" -}}
{{- dig "port" 8080 (fromYaml (include "tilegroxy.server" .)) -}}
{{- end }}

{{- define "tilegroxy.healthEnabled" -}}
{{- dig "health" "enabled" false (fromYaml (include "tilegroxy.server" .)) -}}
{{- end }}

{{- define "tilegroxy.healthPort" -}}
{{- dig "health" "port" 3000 (fromYaml (include "tilegroxy.server" .)) -}}
{{- end }}

{{/*
Encryption settings, if any. A non-empty result means the server terminates TLS
itself on the main port and, when httpport is set, also answers the ACME
challenge and redirects on a second port.
*/}}
{{- define "tilegroxy.encrypt" -}}
{{- dig "encrypt" dict (fromYaml (include "tilegroxy.server" .)) | toYaml -}}
{{- end }}

{{- define "tilegroxy.httpPort" -}}
{{- dig "httpport" 0 (fromYaml (include "tilegroxy.encrypt" .)) -}}
{{- end }}

{{/*
Environment variables shared by the server Deployment and the seed Job:
plain values from .Values.env, then every key of .Values.secrets and
.Values.existingSecretKeys sourced from a Secret.
*/}}
{{- define "tilegroxy.env" -}}
{{- range $k, $v := .Values.env }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- range $k, $v := .Values.secrets }}
- name: {{ $k }}
  valueFrom:
    secretKeyRef:
      name: {{ include "tilegroxy.secretName" $ }}
      key: {{ $k }}
{{- end }}
{{- range $k, $ref := .Values.existingSecretKeys }}
- name: {{ $k }}
  valueFrom:
    secretKeyRef:
      name: {{ $ref.name }}
      key: {{ default $k $ref.key }}
{{- end }}
{{- with .Values.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Volumes and mounts shared by the server Deployment and the seed Job. The config
is always mounted; persistence and extra volumes are opt in.
*/}}
{{- define "tilegroxy.volumes" -}}
- name: config
  configMap:
    name: {{ include "tilegroxy.configMapName" . }}
{{- if .Values.persistence.enabled }}
- name: data
  persistentVolumeClaim:
    claimName: {{ default (printf "%s-data" (include "tilegroxy.fullname" .)) .Values.persistence.existingClaim }}
{{- end }}
{{- with .Values.extraVolumes }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "tilegroxy.volumeMounts" -}}
- name: config
  mountPath: {{ dir .Values.configPath }}
  readOnly: true
{{- if .Values.persistence.enabled }}
- name: data
  mountPath: {{ .Values.persistence.mountPath }}
{{- end }}
{{- with .Values.extraVolumeMounts }}
{{ toYaml . }}
{{- end }}
{{- end }}
