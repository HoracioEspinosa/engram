---
type: runbook
id: RB-002
title: Sesión huérfana no se limpia al cerrar el estanque
service: koi-garden
category: auth
pattern: auth-access
severity: P3
status: outdated
symptoms:
  - El contador de "usuarios conectados" del panel nunca baja aunque todos cerraron sesión
  - Cerrar la pestaña sin usar el botón "Salir" deja la sesión marcada como activa
  - Reaparece un aviso de "sesión duplicada" al volver a entrar desde el mismo dispositivo
owner: equipo-koi-garden
automation_level: manual
last_updated: 2026-02-01
last_verified: 2026-02-10
---

# Sesión huérfana no se limpia al cerrar el estanque

## Síntomas

El panel de administración reporta más sesiones activas que usuarios reales
conectados. Cerrar la pestaña del navegador sin pasar por "Salir" deja la fila
de sesión en estado `active` indefinidamente.

## Diagnóstico

No había un job de barrido que expirara sesiones sin heartbeat reciente; la
limpieza dependía por completo del botón "Salir", que no cubre cierres
abruptos de pestaña o pérdida de red.

## Pasos de mitigación

1. Ejecutar manualmente el barrido de sesiones sin heartbeat en los últimos 30 minutos.
2. Confirmar que el contador de "usuarios conectados" del panel baja al valor real.

## Verificación

Este runbook está marcado `outdated`: su `last_verified` es de 2026-02-10 y no
se ha vuelto a confirmar desde entonces. Antes de aplicarlo, revalidar que el
job de barrido automático (si ya se implementó) no lo haya vuelto innecesario.
