---
type: runbook
id: RB-004
title: Miniaturas corruptas tras la migración de thumbnails
service: tsukimi-bridge
category: data-integrity
pattern: file-save-failure
severity: P1
status: draft
symptoms:
  - Algunas miniaturas de la galería se ven como un cuadro gris o roto
  - El archivo original abre bien, pero su miniatura pesa 0 bytes
  - El problema apareció después de correr la migración de thumbnails de TSU-204
owner: equipo-tsukimi-bridge
automation_level: manual
last_updated: 2026-09-10
---

# Miniaturas corruptas tras la migración de thumbnails

## Síntomas

Tras correr la migración de miniaturas de la tarea TSU-204, una parte de las
imágenes de la galería muestra un ícono de imagen rota. El archivo original
sigue intacto; el problema está solo en el thumbnail regenerado.

## Diagnóstico

Este runbook está en estado `draft`: la migración de TSU-204 todavía tiene
pendientes de validar en `tsukimi-bridge/TSU-204-migracion-thumbnails/`.
La hipótesis actual es que el proceso de regeneración corta la escritura del
archivo cuando la miniatura original pesa más de cierto umbral.

## Pasos de mitigación

1. Identificar las miniaturas con tamaño 0 bytes junto al archivo original.
2. Volver a generar la miniatura para esos archivos puntuales.
3. Documentar aquí el umbral exacto una vez confirmado, para pasar este
   runbook de `draft` a `verified`.

## Verificación

Pendiente: este runbook se marca `draft` hasta que la causa se confirme con
datos de al menos dos corridas de la migración.
