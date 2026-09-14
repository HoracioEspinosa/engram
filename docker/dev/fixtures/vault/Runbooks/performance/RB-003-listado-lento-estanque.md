---
type: runbook
id: RB-003
title: Listado de peces del estanque responde lento
service: koi-garden-pond-02
category: performance
pattern: other
severity: P2
status: verified
symptoms:
  - El listado de peces tarda más de 3 segundos en cargar en la instancia pond-02
  - El indicador de carga queda girando antes de mostrar resultados
  - El problema es más notorio con el filtro de "todos los estanques" activo
owner: equipo-koi-garden
automation_level: assisted
last_updated: 2026-08-30
last_verified: 2026-09-05
---

# Listado de peces del estanque responde lento

## Síntomas

En la instancia `koi-garden-pond-02`, el listado principal de peces tarda
varios segundos en responder cuando el filtro "todos los estanques" está
activo, muy por encima del resto de las instancias.

## Diagnóstico

La consulta de listado no usaba el índice por estanque cuando el filtro
combinaba varios estanques a la vez, forzando un recorrido completo de la
tabla de peces en la instancia con más registros.

## Pasos de mitigación

1. Confirmar en el panel de métricas que la latencia p95 del listado supera 1.5s.
2. Revisar el plan de consulta de la vista de listado combinado.
3. Si el índice compuesto no está presente, aplicar la migración correspondiente.

## Verificación

Repetir el listado con "todos los estanques" activo y confirmar que la
latencia p95 vuelve a estar por debajo de 500ms.
