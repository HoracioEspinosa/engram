# TSU-204 — Migración de miniaturas

**Estado:** Con pendientes
**Proyecto:** tsukimi-bridge
**Ticket:** TSU-204

Migrar la generación de miniaturas de la galería de koi-garden al nuevo
pipeline de tsukimi-bridge. La migración corre, pero dejó una parte de las
miniaturas corruptas (ver `Runbooks/data-integrity/RB-004-thumbnails-corruptos.md`,
todavía en `draft`); falta confirmar la causa exacta antes de dar por
cerrada la tarea.

## Contenido

- `plans/migracion.md` — plan de migración por lotes.
- `benchmarks/` — línea base de tiempo de generación de miniaturas.
- `evidences/01-antes-despues/` — galería antes/después de migrar, con
  manifest de qué prueba cada captura.

## Pendiente

Confirmar el umbral de tamaño que dispara la corrupción de miniaturas
(RB-004) antes de correr la migración sobre el resto del catálogo.
