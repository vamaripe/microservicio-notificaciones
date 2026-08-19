# design-software-notification-db

Capa de datos del **notification-service** (capacidad transversal de entrega de
notificaciones, email / in-app). Extraído de `monitoring` por [ADR-005].

- Schema: `notification`
- Tablas: `notification_template`, `sent_notification`
- Motor: PostgreSQL 16 · Liquibase (estructura por capas `01_ddl`…`05_rollbacks`)
- Additivo: crea un schema nuevo; **no** toca los demás. Se aplica sobre la BD ya existente.

## Aplicar (desde docker-infra)

```
POSTGRES_PORT=15432 docker compose -p ds-develop --env-file .env.develop --profile tooling \
  run --rm liquibase-notification update
```

## Convenciones

- Tablas sin FK en `03_tables`; FKs en `04_alter` (regla de oro).
- Ids de changeset únicos por repo (`notification-*`) para no colisionar con la
  `public.databasechangelog` compartida.
- DCL least-privilege: roles `notification_reader/writer/admin`.
