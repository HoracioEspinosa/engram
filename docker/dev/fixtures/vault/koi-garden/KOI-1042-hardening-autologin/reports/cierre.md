# Reporte de cierre — KOI-1042

**Cerrado:** 2026-08-14

## Resultado

El fix de TTL (`patches/autologin-token-ttl.patch`) se validó en las tres
instancias de koi-garden. El autologin vuelve a entrar directo al panel del
estanque sin pasar por la pantalla de acceso manual.

## Evidencia

- Antes/después del fix en `evidences/01-login-antes/` y
  `evidences/02-login-despues/`.
- Regresión de QA sobre el listado principal en `evidences-qa/01-regresion/`,
  sin efectos colaterales detectados.

## Seguimiento

El runbook `Runbooks/auth/RB-001-autologin-token-expirado.md` documenta el
síntoma y el diagnóstico para el equipo de soporte, por si vuelve a aparecer
en una instancia nueva.
