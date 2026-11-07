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
{{- if and .Values.renderStore.enabled (not .Values.renderStore.url) -}}
{{- fail "renderStore.url is required when renderStore.enabled is true" -}}
{{- end -}}
{{- end -}}

{{/* Takes a component values block. */}}
{{- define "convoy.scheduling" -}}
{{- with .nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .affinity }}
affinity:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{/* Takes a dict with "root", "component" and "values"; renders the controller manager or observer Deployment. */}}
{{- define "convoy.managerDeployment" -}}
{{- $root := .root -}}
{{- $v := .values -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: convoy-{{ .component }}
  namespace: {{ $root.Release.Namespace }}
  labels:
    {{- include "convoy.componentLabels" (dict "root" $root "component" .component) | nindent 4 }}
spec:
  replicas: {{ $v.replicas }}
  selector:
    matchLabels:
      {{- include "convoy.selectorLabels" .component | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "convoy.componentLabels" (dict "root" $root "component" .component) | nindent 8 }}
      {{- with $v.podAnnotations }}
      annotations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
    spec:
      serviceAccountName: convoy-{{ .component }}
      {{- with $root.Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      securityContext:
        {{- include "convoy.podSecurityContext" $root | nindent 8 }}
      containers:
        - name: {{ .component }}
          image: {{ include "convoy.image" (dict "root" $root "component" .component) }}
          imagePullPolicy: {{ $root.Values.image.pullPolicy }}
          args:
            - --shards={{ $root.Values.shards }}
            - --workers={{ $v.workers }}
            - --resync={{ $v.resync }}
            # Each replica caches only its owned shards, so memory scales with replica count.
            - --shard-scoped-cache
            {{- if eq .component "observer" }}
            - --serve-addr=:8080
            {{- end }}
            {{- if $root.Values.renderStore.enabled }}
            - --render-store-url={{ $root.Values.renderStore.url }}
            {{- end }}
            {{- range $v.extraArgs }}
            - {{ . }}
            {{- end }}
          env:
            # Helm and git clients keep their caches under $HOME.
            - name: HOME
              value: /tmp
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            {{- if eq .component "observer" }}
            # Advertised on the member lease, so the apiserver reaches the shard owner.
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            {{- end }}
          {{- if and $root.Values.renderStore.enabled $root.Values.renderStore.existingSecret }}
          envFrom:
            - secretRef:
                name: {{ $root.Values.renderStore.existingSecret }}
          {{- end }}
          {{- if eq .component "observer" }}
          ports:
            - name: http
              containerPort: 8080
          {{- end }}
          {{- with $v.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          securityContext:
            {{- include "convoy.containerSecurityContext" $root | nindent 12 }}
          volumeMounts:
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: tmp
          emptyDir: {}
      {{- include "convoy.scheduling" $v | nindent 6 }}
{{- end -}}
