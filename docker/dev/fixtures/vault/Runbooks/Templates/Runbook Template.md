---
type: runbook
id: RB-000
service: <slug-del-servicio>
category: <auth|database|queue|network|performance|data-integrity|registration>
pattern: <missing-files|auth-access|file-save-failure|sync-upload|registration-subscription|other>
severity: <P1|P2|P3|P4>
status: <draft|verified|outdated>
symptoms:
  - <síntoma observable, uno por línea>
  - <otro síntoma>
tags: [template]
owner: <equipo-responsable>
automation_level: <manual|assisted|autonomous-with-gate>
last_updated: <YYYY-MM-DD>
last_verified: <YYYY-MM-DD>
---

# <Título del runbook>

Esta nota es la plantilla base para un runbook nuevo. Vive bajo `Runbooks/Templates/`
a propósito: el escáner del vault (`internal/runbooks/vault_scan.go`) salta cualquier
nota bajo ese prefijo antes de mirar su frontmatter, así que esta plantilla nunca
termina indexada como runbook real aunque su `type` diga `runbook`.

## Síntomas

Copia los síntomas del frontmatter en prosa, con el detalle que un operador
necesita para reconocer el problema sin abrir un ticket.

## Diagnóstico

Explica la causa raíz confirmada y cómo se llegó a ella (logs, trazas, métricas).

## Pasos de mitigación

1. Primer paso accionable.
2. Segundo paso accionable.
3. Verificación de que el síntoma desapareció.

## Verificación

Cómo confirmar que el runbook sigue vigente (comando, panel, query) y cada
cuánto se espera revisarlo.
