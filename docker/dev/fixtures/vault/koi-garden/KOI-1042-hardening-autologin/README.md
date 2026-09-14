# KOI-1042 — Hardening del autologin

**Estado:** Cerrado (2026-08-14)
**Proyecto:** koi-garden
**Ticket:** KOI-1042

Endurecer el autologin de koi-garden tras detectar que una parte de los
usuarios con sesión recordada caía en la pantalla de acceso en vez de entrar
directo al panel del estanque. La causa raíz fue un TTL de token mal
calculado; el fix se aplicó, se verificó en las tres instancias y se cerró
con evidencia de antes/después.

## Contenido

- `analysis/` — causa raíz confirmada.
- `plans/` — plan de ataque seguido para el fix.
- `evidences/` — capturas y respuesta cruda de antes y después del fix.
- `evidences-qa/` — evidencia de la regresión de QA sobre el listado, para
  confirmar que el fix no rompió nada adyacente.
- `patches/` — el parche aplicado.
- `reports/` — reporte de cierre.
