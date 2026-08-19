# HU-01 — Contrato de API y DTOs del microservicio de notificaciones

## Objetivo

La carpeta `api/` centraliza los contratos de entrada y salida del servicio, generados desde contratos compartidos (`shared-contracts`) y consumidos por los adapters HTTP y AMQP.

Su propósito principal es mantener un diseño de tipo **contract-first**: la API y los eventos se definen antes que la implementación, y el código Go se genera a partir de esos contratos para evitar divergencias entre la especificación y la lógica del servicio.

Este paquete es la capa de intercambio de datos entre:

- el cliente HTTP
- el adapter HTTP (`internal/adapter/in/http`)
- el consumidor AMQP (`internal/adapter/in/amqp`)
- los contratos generados por OpenAPI y JSON Schema

---

## ¿Qué contiene esta carpeta?

La carpeta `api/` tiene exactamente 3 archivos relevantes:

1. `doc.go` — documentación del paquete y comandos de regeneración.
2. `notification.gen.go` — tipos generados para la API HTTP de notificaciones.
3. `event_envelope.gen.go` — tipos generados para el envelope de eventos de dominio.

Estos tres archivos no se editan a mano. Se generan automáticamente desde contratos externos y se tratan como artefactos de integración.

---

## 1) `doc.go`

### Responsabilidad

Este archivo no define lógica de negocio, sino la metadata del paquete y la guía de generación. Corrige el contexto de cómo se crea el contrato.

### Qué indica

- La carpeta `api` contiene DTOs generados a partir de `shared-contracts`.
- No deben editarse manualmente.
- `notification.gen.go` se genera desde OpenAPI:

```bash
oapi-codegen -generate types -package api -o api/notification.gen.go \
  ../design-software-shared-contracts/openapi/notification.yaml
```

- `event_envelope.gen.go` se genera desde el JSON Schema del envelope de evento:

```bash
go-jsonschema -p api -t --capitalization ID -o api/event_envelope.gen.go \
  ../design-software-shared-contracts/events/event-envelope.schema.json
```

### Nota importante

El comentario documenta un problema real del contrato compartido: el schema del envelope de eventos tenía un escape regex incorrecto (`\.` en lugar de `\\.` en JSON). Eso impedía que `go-jsonschema` lo parseara correctamente. Por eso el archivo generado se obtuvo con una corrección local del contrato, y no se debe tocar aquí.

### Importancia para HU-01

Este archivo explica la base del enfoque de la HU-01: la API no está diseñada ad hoc, sino desde un contrato compartido y verificable.

---

## 2) `notification.gen.go`

### Responsabilidad

Define los modelos de entrada y salida usados por la API HTTP.

### Modelo principal: `SendNotificationRequest`

```go
type SendNotificationRequest struct {
    Channel        SendNotificationRequestChannel `json:"channel"`
    RecipientEmail openapi_types.Email            `json:"recipient_email"`
    RecipientId    openapi_types.UUID             `json:"recipient_id"`
    SourceEventId  *openapi_types.UUID            `json:"source_event_id,omitempty"`
    SourceService  *string                        `json:"source_service,omitempty"`
    Subject        string                         `json:"subject"`
    TemplateCode   *string                        `json:"template_code,omitempty"`
    TemplateVars   *map[string]string             `json:"template_vars,omitempty"`
}
```

### Campos clave

- `channel`: canal de envío (`EMAIL` o `IN_APP`)
- `recipient_email`: email destino
- `recipient_id`: id del destinatario
- `subject`: asunto o texto base
- `template_code`: código de plantilla opcional
- `template_vars`: variables de plantilla opcionales
- `source_event_id`: id del evento origen, útil para idempotencia
- `source_service`: servicio que originó la operación

### Enum de canales

```go
const (
    SendNotificationRequestChannelEMAIL SendNotificationRequestChannel = "EMAIL"
    SendNotificationRequestChannelINAPP SendNotificationRequestChannel = "IN_APP"
)
```

### Modelo de respuesta: `SentNotification`

```go
type SentNotification struct {
    Channel     *SentNotificationChannel    `json:"channel,omitempty"`
    Id          *openapi_types.UUID         `json:"id,omitempty"`
    RecipientId *openapi_types.UUID         `json:"recipient_id,omitempty"`
    SendStatus  *SentNotificationSendStatus `json:"send_status,omitempty"`
    SentAt      *time.Time                  `json:"sent_at,omitempty"`
    Subject     *string                     `json:"subject,omitempty"`
}
```

### Estados posibles

```go
const (
    FAILED  SentNotificationSendStatus = "FAILED"
    PENDING SentNotificationSendStatus = "PENDING"
    SENT    SentNotificationSendStatus = "SENT"
)
```

### Importancia para HU-01

Este archivo es el corazón del contrato HTTP de la HU-01. Es lo que define qué forma tiene el payload de entrada y cuál es la respuesta esperada al crear una notificación.

---

## 3) `event_envelope.gen.go`

### Responsabilidad

Define el envelope estándar que usa el sistema para eventos de dominio publicados en RabbitMQ.

### Estructura principal

```go
type DomainEventEnvelope struct {
    CorrelationID *string `json:"correlation_id,omitempty"`
    EventID       string  `json:"event_id"`
    EventType     string  `json:"event_type"`
    Payload       DomainEventEnvelopePayload `json:"payload"`
    SourceService string  `json:"source_service"`
    Timestamp     time.Time `json:"timestamp"`
    Version       string  `json:"version"`
}
```

### Campos clave

- `event_id`: identificador único del evento para deduplicación
- `event_type`: nombre del evento, con formato de dominio: `service.entity.action`
- `payload`: contenido del evento
- `source_service`: sistema que emitió el evento
- `timestamp`: instante de emisión
- `version`: versión del envelope
- `correlation_id`: campo opcional para correlación de trazas

### Validación incluida

El archivo genera un `UnmarshalJSON` con validaciones:

- `event_id` es requerido
- `event_type` es requerido
- `payload` es requerido
- `source_service` es requerido
- `timestamp` es requerido
- `version` es requerido
- además `event_type` debe cumplir el patrón:

```regex
^[a-z_]+\.[a-z_]+\.[a-z_]+$
```

Ejemplo válido:

```text
scheduling.schedule.published
monitoring.alert.triggered
```

### Importancia para HU-01

Aunque la HU-01 se centra en `POST /notifications`, este envelope es fundamental para la integración con eventos de negocio del microservicio. Define cómo los eventos llegan al consumidor y cómo el sistema puede mantener trazabilidad e idempotencia.

---

## Relación con la implementación

La parte de HTTP se implementa en:

- `internal/adapter/in/http/handler.go`

Ese adapter hace lo siguiente:

1. Decodifica el JSON en `api.SendNotificationRequest`
2. Valida los campos de negocio
3. Construye el comando de caso de uso
4. Llama a `SendNotification` del dominio de aplicación
5. Devuelve un `api.SentNotification`

### Validación de entrada

En `handler.go` hay una validación adicional manual:

```go
func validateSendRequest(req api.SendNotificationRequest) error {
    if req.RecipientId == uuid.Nil || req.RecipientEmail == "" || req.Subject == "" {
        return errMissingFields
    }
    if !req.Channel.Valid() {
        return errInvalidChannel
    }
    return nil
}
```

Esto corrige un detalle importante: el código generado por OpenAPI valida el tipo, pero no siempre bloquea un JSON que llega con campos nulos o vacíos. Por eso el adapter hace una validación explícita de negocio.

---

## Ejemplo de uso: POST /notifications

### Request

```http
POST /notifications
Content-Type: application/json
```

```json
{
  "channel": "EMAIL",
  "recipient_email": "alumno@ejemplo.com",
  "recipient_id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
  "subject": "Tu horario ha sido publicado",
  "source_service": "academic-service",
  "source_event_id": "e8a3b9d1-1045-4a02-9bbf-2d3f89bc2f1f",
  "template_code": "SCHEDULE_PUBLISHED",
  "template_vars": {
    "schedule_name": "Horario Q1",
    "ficha": "FICHA-2026"
  }
}
```

### Response esperada

```http
202 Accepted
Content-Type: application/json
```

```json
{
  "id": "6f2c0d45-9a7d-46ff-8d95-34f8f04ad2cd",
  "recipient_id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
  "channel": "EMAIL",
  "send_status": "PENDING",
  "subject": "Tu horario ha sido publicado"
}
```

---

## Dimensión de la HU-01

La HU-01 se centra en una obligación funcional muy específica:

> El sistema debe permitir enviar una notificación por API, con contrato claro, validación, persistencia y respuesta controlada.

Es la versión “manual” del flujo de envío. A diferencia del flujo AMQP, aquí el origen es una petición directa del cliente y la aplicación la registra como una notificación explícita.

### Criterios funcionales de HU-01

- La API acepta una notificación válida.
- El canal soportado es `EMAIL` o `IN_APP`.
- Los campos mínimos son validados.
- La operación queda registrada en base de datos.
- El estado inicial es `PENDING`.
- La respuesta confirma la creación con un `id` y su estado.

---

## Conclusión

La carpeta `api/` es la pieza de contrato del sistema. No es una capa de negocio, sino una capa de definición de mensajes y tipos compartidos que guían la interacción del sistema con el exterior.

- `doc.go` explica la política de generación.
- `notification.gen.go` define el contrato HTTP de la notificación.
- `event_envelope.gen.go` define el envelope de eventos de dominio.

Juntos permiten que la HU-01 se ejecute con un diseño más seguro, consistente y mantenible: validación por contrato, serialización estructurada y trazabilidad en la integración entre servicios.
