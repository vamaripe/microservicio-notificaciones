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

## Historias de usuario implementadas y complementos

La siguiente síntesis se basa en los contratos generados, los casos de uso, los adaptadores Go y
las migraciones de la base de datos que existen en este repositorio.

### HU-001 - Enviar notificación vía API (contract-first, `POST /notifications`)

**Qué hace:** expone `POST /notifications` y valida el cuerpo JSON antes de delegar en el caso de
uso `SendNotification`. El contrato define `recipient_id`, `recipient_email`, `channel` (`EMAIL`
o `IN_APP`) y `subject`, además de `template_code`, `template_vars`, `source_event_id` y
`source_service` opcionales. Los DTO se generan desde el contrato OpenAPI compartido y no se editan
a mano. La operación persiste una fila en `notification.sent_notification` con estado inicial
`PENDING` y responde `202` con el identificador y el estado de la notificación.

**Qué se podría complementar:** conectar este endpoint con una entrega efectiva por canal, porque el
flujo HTTP actual persiste la notificación pero la entrega SMTP la dispara el flujo asíncrono del
worker. También se podría completar la idempotencia del propio POST cuando se repita un
`source_event_id`, documentar o probar límites de campos del contrato y añadir pruebas de contrato
contra el documento OpenAPI compartido.

**Cierre:** esta HU estableció la entrada síncrona y contract-first del microservicio para registrar
una solicitud de notificación de forma consistente.

### HU-002 - Consumir evento y entregar notificación (AMQP, idempotencia)

**Qué hace:** el `notification-worker` consume desde RabbitMQ los eventos
`scheduling.schedule.published` y `monitoring.alert.triggered`, resuelve el destinatario mediante
`RecipientResolver`, construye la notificación y usa el adaptador correspondiente. El resolver
actual es un stub configurable para desarrollo. Cuando la entrega es exitosa, guarda la
notificación y crea `notification.notification.sent` en `notification.outbox` dentro de la misma
transacción; el `OutboxRelay` publica después ese evento en `notification-events` y marca
`published_at`. La tabla `sent_notification` tiene un índice único sobre `source_event_id`, por lo
que un evento redeliverado no genera otra notificación.

**Qué se podría complementar:** sustituir el `StubRecipientResolver` por la consulta real al
servicio de actores y definir el fan-out cuando un evento tenga varios instructores. El código
actual notifica al actor `published_by` para horarios y no envía una notificación por cada elemento
de `instructor_ids`. También conviene separar con claridad la confirmación de persistencia de la
entrega externa y ampliar las pruebas de integración con los servicios upstream y downstream.

**Cierre:** esta HU conectó el microservicio con el flujo de eventos del ecosistema y añadió la
persistencia transaccional más la protección contra duplicados.

### HU-003 - Entrega por canal `EMAIL` / `IN_APP`

**Qué hace:** el dominio y la base de datos reconocen los canales `EMAIL` e `IN_APP`. Para `EMAIL`,
`SMTPNotifier` envía el asunto y el cuerpo a la dirección configurada en `NOTIFICATION_SMTP_ADDR`
(MailHog en desarrollo). Para `IN_APP`, `InAppNotifier` no realiza una entrega externa: la fila
persistida en `sent_notification` queda disponible mediante `GET /notifications/{id}`. El
`CompositeNotifier` registra la métrica `notification.delivered` con canal y resultado.

Hay una limitación concreta en el flujo AMQP actual: `ConsumeDomainEvent` crea la notificación con
canal `EMAIL` de forma fija. Por tanto, aunque existe el tipo, el contrato y el adaptador `IN_APP`,
las entregas originadas por esos eventos no seleccionan todavía el canal de la plantilla.

**Qué se podría complementar:** seleccionar el canal desde la plantilla o desde la orden/evento,
implementar el almacenamiento y consulta orientados a bandeja in-app y hacer que cada canal tenga
su entrega y estado propios. También se debería añadir una matriz de pruebas por canal, incluyendo
fallos SMTP y la expectativa de que una notificación `IN_APP` no intente usar SMTP.

**Cierre:** esta HU definió la abstracción multicanal, con entrega EMAIL funcional en desarrollo y
una representación IN_APP basada actualmente en persistencia.

### HU-004 - Resiliencia: reintentos, DLQ e idempotencia

**Qué hace:** la parte implementada es la idempotencia mediante restricciones únicas en base de
datos y el patrón Outbox, que ofrece publicación at-least-once para eventos de salida pendientes.
También existen estados `PENDING`, `SENT` y `FAILED`, y el worker reconoce el mensaje AMQP después
de procesar un envelope válido. Un envelope JSON inválido se rechaza sin reencolarlo.

**Qué falta o se podría complementar:** los reintentos con backoff y la DLQ no están implementados.
No hay una política de máximo de intentos, clasificación entre errores transitorios y permanentes,
ni una cola de mensajes fallidos. Ante un error del caso de uso después de decodificar un envelope,
el consumidor registra el error y hace `Ack`; ante un envelope inválido hace `Nack` sin requeue, por
lo que el mensaje se pierde al no existir una DLQ configurada. Se debería añadir esa topología,
contadores de intentos, backoff, límites, redrive y pruebas de caída del broker, base de datos,
resolver y SMTP.

**Cierre:** esta HU dejó una base de resiliencia con Outbox e idempotencia, pero el ciclo completo
de recuperación ante fallos todavía requiere reintentos controlados y DLQ.

### HU-005 - Consultar notificación enviada (`GET`)

**Qué hace:** expone `GET /notifications/{id}`. Valida que el parámetro sea un UUID, consulta
`notification.sent_notification` mediante `GetNotificationUseCase`, devuelve `200` si existe,
`404 NOT_FOUND` si no existe y `503 DEPENDENCY_UNAVAILABLE` ante un error de persistencia. La
respuesta expone el identificador, destinatario, canal, asunto, estado y `sent_at` según el DTO
generado. Esta ruta también sirve como consulta del registro que representa una notificación
IN_APP en el estado actual.

**Qué se podría complementar:** añadir consultas por destinatario, estado, canal o rango de fechas,
con paginación y ordenamiento, si el contrato del producto las requiere. También se podría ocultar
o limitar datos sensibles del destinatario, definir una política de autorización y ampliar las
pruebas para errores de formato, ausencia, estados `PENDING`/`FAILED` y notificaciones ya enviadas.

**Cierre:** esta HU proporcionó la lectura puntual del resultado persistido de una notificación y
sus estados de entrega.

### HU-006 - Plantillas de notificación

**Qué hace:** mantiene plantillas reutilizables en `notification.notification_template`, con código
único, canal, asunto, cuerpo y bandera `is_active`. Hay semillas para `SCHEDULE_PUBLISHED` y
`ALERT_TRIGGERED`. En los eventos soportados, el caso de uso busca la plantilla activa y reemplaza
variables `{{schedule_name}}`, `{{ficha}}` o `{{alert_type}}` mediante `TemplateRenderer`; además
guarda `template_id` en la notificación. Si la plantilla no existe o está inactiva, conserva el
asunto base del evento. El POST también acepta `template_code` y `template_vars` en su contrato,
aunque el caso de uso HTTP actual no usa esos campos para renderizar.

**Qué se podría complementar:** hacer efectivo el uso de plantillas en el flujo HTTP, validar que
las variables requeridas estén presentes, escapar valores no confiables y definir qué ocurre ante
errores de renderización. También se podría agregar administración versionada de plantillas,
validación de compatibilidad entre plantilla y canal, y pruebas para plantillas inexistentes,
inactivas y variables faltantes. No se debería asumir que las semillas resuelven la administración
completa del catálogo.

**Cierre:** esta HU incorporó contenido parametrizable y persistido para los eventos conocidos, con
un fallback explícito cuando no hay una plantilla activa.

### HU-007 - Observabilidad OpenTelemetry (traces, metrics, logs) y health

**Qué hace:** configura exportación OTLP/gRPC al Collector y añade trazas HTTP con `otelhttp`, spans
manuales para consumo y publicación AMQP, y trazas de consultas PostgreSQL mediante `otelpgx`. El
contexto W3C `traceparent` se extrae de los headers AMQP, se conserva en el payload del Outbox y se
inyecta al publicar el evento de salida. Expone métricas RED HTTP y
`notification.delivered{channel,status}`. Los logs usan `slog` estructurado e incorporan
`trace_id` y `span_id` cuando existe un span activo. `GET /health` comprueba liveness y `GET /ready`
comprueba las dependencias registradas; el worker expone su health en el puerto configurado,
normalmente `8081`.

**Qué se podría complementar:** definir dashboards y alertas operativas sobre las métricas
existentes, añadir métricas explícitas de reintentos, DLQ, edad del Outbox y latencia de entrega,
centralizar logs en el entorno destino y establecer objetivos de disponibilidad/latencia. También
se podrían probar de forma integrada las trazas completas desde los productores hasta los
consumidores downstream.

**Cierre:** esta HU hizo observable el recorrido síncrono y asíncrono del microservicio y añadió
señales básicas para salud, diagnóstico y operación.

### HU-008 - Levantado local end-to-end (`docker-infra`)

**Qué hace:** `docker-infra/docker-compose.yml` levanta PostgreSQL, RabbitMQ, MailHog y el OpenTelemetry
Collector; Liquibase aplica las migraciones de la base de datos. El README raíz documenta cómo
iniciar la API, iniciar el worker, consultar `/health` y `/ready`, ejecutar `POST /notifications`,
publicar un evento por la API de RabbitMQ, revisar el correo en MailHog, consultar las tablas
`sent_notification` y `outbox`, y revisar los logs del Collector. El flujo verificable es
evento AMQP -> worker -> SMTP/MailHog -> estado `SENT` -> Outbox -> publicación del evento de
salida.

**Qué se podría complementar:** automatizar esta secuencia como prueba end-to-end reproducible,
esperar explícitamente a que las dependencias estén listas antes de iniciar API y worker y añadir
un caso de fallo que demuestre el comportamiento de `FAILED`, reintentos y DLQ cuando esas
capacidades existan. También se podría documentar la limpieza del entorno y separar claramente
valores de desarrollo de cualquier configuración destinada a otros ambientes.

**Cierre:** esta HU integró las piezas del microservicio en un escenario local comprobable, desde la
infraestructura hasta la entrega y publicación del evento resultante.

### Cierre general del microservicio

En conjunto, el microservicio de notificaciones recibe solicitudes HTTP contract-first y eventos
AMQP, persiste notificaciones en PostgreSQL, entrega EMAIL mediante SMTP en el worker, representa
IN_APP mediante la consulta del registro persistido, reutiliza plantillas para los eventos
soportados y publica eventos de salida mediante Outbox. La idempotencia, los health checks y la
observabilidad OpenTelemetry están implementados. El alcance todavía debe completarse en entrega
multicanal efectiva, resolución real de destinatarios, reintentos con backoff, DLQ, uso de
plantillas desde el POST y pruebas operativas end-to-end automatizadas.
