## Prueba manual local (PowerShell)

La prueba requiere tres terminales: una para la infraestructura, una para la API y otra para
el worker. Ejecuta los comandos desde la raiz del repositorio, salvo donde se indique lo
contrario.

### 1. Levantar la infraestructura

```powershell
docker compose --env-file docker-infra/.env.develop -f docker-infra/docker-compose.yml up -d postgres rabbitmq mailhog otel-collector
docker compose --env-file docker-infra/.env.develop -f docker-infra/docker-compose.yml --profile tooling run --rm liquibase-notification update
```

Comprueba que los contenedores esten activos:

```powershell
docker compose --env-file docker-infra/.env.develop -f docker-infra/docker-compose.yml ps
```

### 2. Iniciar la API

En la segunda terminal:

```powershell
cd design-software-notification-service-develop
$env:NOTIFICATION_DB_DSN="postgres://design_software_user:contrasena@localhost:5432/design-software-develop?sslmode=disable"
$env:OTEL_EXPORTER_OTLP_ENDPOINT="localhost:4317"
$env:OTEL_EXPORTER_OTLP_INSECURE="true"
go run ./cmd/notification-api
```

### 3. Iniciar el worker

En la tercera terminal:

```powershell
cd design-software-notification-service-develop
$env:NOTIFICATION_DB_DSN="postgres://design_software_user:contrasena@localhost:5432/design-software-develop?sslmode=disable"
$env:NOTIFICATION_AMQP_URL="amqp://design_software_user:contrasena@127.0.0.1:5672/"
$env:NOTIFICATION_SMTP_ADDR="localhost:1025"
$env:NOTIFICATION_SMTP_FROM="notifications@sena.local"
$env:OTEL_EXPORTER_OTLP_ENDPOINT="localhost:4317"
$env:OTEL_EXPORTER_OTLP_INSECURE="true"
$env:WORKER_HEALTH_PORT="8081"
go run ./cmd/notification-worker
```

### 4. Comprobar salud

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/ready
Invoke-RestMethod http://localhost:8081/health
Invoke-RestMethod http://localhost:8081/ready
```

### 5. Probar el endpoint HTTP

```powershell
$sourceEventId = [guid]::NewGuid().ToString()
$body = @{
  channel = "EMAIL"
  recipient_email = "aprendiz-prueba@sena.local"
  recipient_id = "22222222-2222-4222-8222-222222222222"
  source_event_id = $sourceEventId
  source_service = "manual-integration-test"
  subject = "Prueba notification-service"
} | ConvertTo-Json

$created = Invoke-RestMethod http://localhost:8080/notifications -Method Post `
  -ContentType "application/json" -Body $body
$created
Invoke-RestMethod "http://localhost:8080/notifications/$($created.id)"
```

La respuesta esperada del `POST` es `202` y `send_status: PENDING`. Este endpoint persiste una
notificacion; la entrega SMTP la dispara un evento AMQP consumido por el worker. Genera siempre un
`source_event_id` nuevo; la base de datos lo usa para impedir notificaciones duplicadas.

### 6. Probar el flujo completo mediante RabbitMQ

El siguiente comando publica un evento `monitoring.alert.triggered` en RabbitMQ:

```powershell
$eventId = [guid]::NewGuid().ToString()
$event = @{
  event_id = $eventId
  event_type = "monitoring.alert.triggered"
  version = "1.0"
  timestamp = (Get-Date).ToUniversalTime().ToString("o")
  source_service = "manual-integration-test"
  payload = @{
    affected_entity_type = "Learner"
    affected_entity_id = "10101010-1010-1010-1010-101010101010"
    alert_type_code = "LOW_ATTENDANCE"
  }
} | ConvertTo-Json -Depth 6 -Compress

$publish = @{
  routing_key = "monitoring.alert.triggered"
  payload = $event
  payload_encoding = "string"
  properties = @{ content_type = "application/json"; delivery_mode = 2 }
} | ConvertTo-Json -Depth 6

$credential = New-Object System.Management.Automation.PSCredential(
  "design_software_user",
  (ConvertTo-SecureString "contrasena" -AsPlainText -Force)
)

Invoke-RestMethod "http://localhost:15672/api/exchanges/%2F/monitoring-events/publish" `
  -Method Post -Authentication Basic -Credential $credential `
  -AllowUnencryptedAuthentication -ContentType "application/json" -Body $publish
```

La respuesta debe indicar `routed: true`. Verifica el correo en [MailHog](http://localhost:8025)
y consulta la base de datos:

```powershell
docker exec design-software-db psql -U design_software_user -d design-software-develop -c `
  "SELECT id, source_event_id, send_status, channel, recipient_email, subject, sent_at FROM notification.sent_notification ORDER BY created_at DESC LIMIT 5;"

docker exec design-software-db psql -U design_software_user -d design-software-develop -c `
  "SELECT event_id, event_type, published_at, payload->>'notification_id' AS notification_id FROM notification.outbox ORDER BY created_at DESC LIMIT 5;"
```

El flujo completo esperado es: evento AMQP -> worker -> envio SMTP a MailHog -> estado `SENT`
-> registro en `notification.outbox` -> publicacion del evento de salida.

Para revisar los logs del Collector:

```powershell
docker logs design-software-otel-collector
```
