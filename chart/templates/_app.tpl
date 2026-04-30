{{- define "backupgate.image" -}}
{{- $image := (include "common.images.image" (dict "imageRoot" .Values.image "global" .Values.global)) }}
{{- if eq .Values.image.tag "" -}}
{{- $image = (printf "%s%s" $image .Chart.AppVersion) -}}
{{- end -}}
{{- $image -}}
{{- end -}}
{{- define "backupgate.imagePullSecrets" -}}
{{- include "common.images.pullSecrets" (dict "images" (list .Values.image) "global" .Values.global) -}}
{{- end -}}
{{- define "backupgate.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "common.names.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}
{{- define "backupgate.configMapName" -}}
{{- if .Values.existingConfigMap -}}
{{- .Values.existingConfigMap -}}
{{- else -}}
{{ template "common.names.fullname" . }}-config
{{- end -}}
{{- end -}}

{{- define "backupgate.resources.requests" }}
{{- $def := dict -}}
{{- merge .Values.resources.requests $def | toYaml -}}
{{- end }}

{{- define "backupgate.cacheStorage.pvcName" }}
{{- printf "%s-cache" (include "common.names.fullname" .) }}
{{- end }}

{{- define "backupgate.configmapName" -}}
{{- printf "%s-config" (include "common.names.fullname" .) -}}
{{- end }}

