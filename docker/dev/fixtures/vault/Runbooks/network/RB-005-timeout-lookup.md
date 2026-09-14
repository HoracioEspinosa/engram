---
type: runbook
id: RB-005
title: Timeout en el lookup de peces entre estanques
service: koi-garden-pond-02
category: network
pattern: other
severity: P1
status: verified
symptoms:
  - Buscar un pez por su código entre estanques falla con timeout a los 30 segundos
  - El error solo ocurre cuando el lookup cruza de pond-02 a otro estanque
  - Los reintentos automáticos no mejoran la tasa de éxito
owner: equipo-koi-garden
automation_level: assisted
last_updated: 2026-09-02
last_verified: 2026-09-08
---

# Timeout en el lookup de peces entre estanques

## Síntomas

El buscador de peces por código, cuando resuelve entre estanques distintos,
se cuelga y termina en timeout a los 30 segundos. Ocurre de forma consistente
al buscar desde `koi-garden-pond-02`.

## Diagnóstico

Ver el análisis completo en
`koi-garden/KOI-1099-lookup-timeout/analysis/hipotesis.md`: el cliente de
lookup no tenía un timeout propio configurado y heredaba el límite por
defecto del transporte HTTP, mucho más alto que lo tolerable para el usuario.

## Pasos de mitigación

1. Confirmar que el timeout del cliente de lookup está configurado explícitamente.
2. Mientras se aplica el fix definitivo, seguir los pasos de
   `koi-garden/KOI-1099-lookup-timeout/runbooks/pasos-mitigacion.md`.
3. Validar la corrección también en la instancia pond-02, no solo en la 01.

## Verificación

Repetir una búsqueda cruzada de estanques y confirmar que resuelve en menos
de 2 segundos, con los datos de `benchmarks/baseline-run2.json` como referencia.
