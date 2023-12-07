{{/* singleBinary replicas calculation */}}
{{- define "loki.singleBinaryReplicas" -}}
{{- $replicas := 1 }}
{{- $usingObjectStorage := eq (include "loki.isUsingObjectStorage" .) "true" }}
{{- if and $usingObjectStorage (gt (int .Values.singleBinary.replicas) 1)}}
{{- $replicas = int .Values.singleBinary.replicas -}}
{{- end }}
{{- printf "%d" $replicas }}
{{- end }}
