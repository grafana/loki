Verify the application is working by running these commands:
  kubectl --namespace {{ .Release.Namespace }} port-forward service/{{ include "loki.fullname" . }} {{ .Values.service.port }}
  curl http://127.0.0.1:{{ .Values.service.port }}/api/prom/label
