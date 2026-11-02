{{- define "convoy.labels" -}}
app.kubernetes.io/part-of: convoy
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* Takes a dict with "root" and "component". */}}
{{- define "convoy.componentLabels" -}}
{{ include "convoy.labels" .root }}
app.kubernetes.io/name: convoy-{{ .component }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "convoy.selectorLabels" -}}
app.kubernetes.io/name: convoy-{{ . }}
{{- end -}}

{{- define "convoy.image" -}}
{{- $tag := .root.Values.image.tag | default (printf "v%s" .root.Chart.AppVersion) -}}
{{ printf "%s/convoy-%s:%s" .root.Values.image.registry .component $tag }}
{{- end -}}

{{- define "convoy.observerURL" -}}
http://convoy-observer.{{ .Release.Namespace }}.svc:8080
{{- end -}}

{{- define "convoy.servingSecret" -}}
{{- .Values.tls.existingSecret | default "convoy-apiserver-tls" -}}
{{- end -}}

{{- define "convoy.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "convoy.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end -}}

{{/* Fails the render early on values combinations that would deploy a broken apiserver. */}}
{{- define "convoy.validate" -}}
{{- if and (not .Values.tls.insecure) (not .Values.tls.certManager.enabled) -}}
{{- if or (not .Values.tls.existingSecret) (not .Values.tls.caBundle) -}}
{{- fail "tls.existingSecret and tls.caBundle are required when tls.insecure and tls.certManager.enabled are false" -}}
{{- end -}}
{{- end -}}
{{- if not .Values.etcd.enabled -}}
{{- if or (not .Values.etcd.external.servers) (not .Values.etcd.external.clientSecret) -}}
{{- fail "etcd.external.servers and etcd.external.clientSecret are required when etcd.enabled is false" -}}
{{- end -}}
{{- end -}}
{{- if and .Values.renderStore.existingSecret (not .Values.renderStore.url) -}}
{{- fail "renderStore.url is required when renderStore.existingSecret is set" -}}
{{- end -}}
{{- end -}}
