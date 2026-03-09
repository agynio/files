{{- define "files.configureEnv" -}}
{{- $env := list -}}

{{- $httpAddress := trimAll " \n\t" (default ":8080" .Values.files.httpAddress) -}}
{{- if $httpAddress }}
{{- $env = append $env (dict "name" "HTTP_ADDRESS" "value" $httpAddress) -}}
{{- end }}

{{- $dbSecret := trim (default "" .Values.files.databaseUrl.existingSecret) -}}
{{- $dbVar := dict "name" "DATABASE_URL" -}}
{{- if $dbSecret }}
  {{- $dbKey := default "database-url" .Values.files.databaseUrl.existingSecretKey -}}
  {{- $_ := set $dbVar "valueFrom" (dict "secretKeyRef" (dict "name" $dbSecret "key" $dbKey)) -}}
{{- else }}
  {{- $dbValue := required "files.databaseUrl.value is required" (trimAll " \n\t" (default "" .Values.files.databaseUrl.value)) -}}
  {{- $_ := set $dbVar "value" $dbValue -}}
{{- end }}
{{- $env = append $env $dbVar -}}

{{- $endpoint := required "files.s3.endpoint is required" (trimAll " \n\t" (default "" .Values.files.s3.endpoint)) -}}
{{- $env = append $env (dict "name" "S3_ENDPOINT" "value" $endpoint) -}}

{{- $bucket := required "files.s3.bucket is required" (trimAll " \n\t" (default "" .Values.files.s3.bucket)) -}}
{{- $env = append $env (dict "name" "S3_BUCKET" "value" $bucket) -}}

{{- $region := trimAll " \n\t" (default "us-east-1" .Values.files.s3.region) -}}
{{- $env = append $env (dict "name" "S3_REGION" "value" $region) -}}

{{- $useSSL := printf "%t" (default false .Values.files.s3.useSSL) -}}
{{- $env = append $env (dict "name" "S3_USE_SSL" "value" $useSSL) -}}

{{- $accessSecret := trim (default "" .Values.files.s3.accessKey.existingSecret) -}}
{{- $accessVar := dict "name" "S3_ACCESS_KEY" -}}
{{- if $accessSecret }}
  {{- $accessKey := default "s3-access-key" .Values.files.s3.accessKey.existingSecretKey -}}
  {{- $_ := set $accessVar "valueFrom" (dict "secretKeyRef" (dict "name" $accessSecret "key" $accessKey)) -}}
{{- else }}
  {{- $accessValue := required "files.s3.accessKey.value is required" (trimAll " \n\t" (default "" .Values.files.s3.accessKey.value)) -}}
  {{- $_ := set $accessVar "value" $accessValue -}}
{{- end }}
{{- $env = append $env $accessVar -}}

{{- $secretSecret := trim (default "" .Values.files.s3.secretKey.existingSecret) -}}
{{- $secretVar := dict "name" "S3_SECRET_KEY" -}}
{{- if $secretSecret }}
  {{- $secretKey := default "s3-secret-key" .Values.files.s3.secretKey.existingSecretKey -}}
  {{- $_ := set $secretVar "valueFrom" (dict "secretKeyRef" (dict "name" $secretSecret "key" $secretKey)) -}}
{{- else }}
  {{- $secretValue := required "files.s3.secretKey.value is required" (trimAll " \n\t" (default "" .Values.files.s3.secretKey.value)) -}}
  {{- $_ := set $secretVar "value" $secretValue -}}
{{- end }}
{{- $env = append $env $secretVar -}}

{{- $maxSize := int (default 20971520 .Values.files.maxFileSize) -}}
{{- $env = append $env (dict "name" "MAX_FILE_SIZE" "value" (printf "%d" $maxSize)) -}}

{{- $userEnv := .Values.env | default (list) -}}
{{- $_ := set .Values "env" (concat $env $userEnv) -}}
{{- end -}}
