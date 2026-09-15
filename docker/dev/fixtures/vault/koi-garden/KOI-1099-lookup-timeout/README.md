# KOI-1099 — Timeout de lookup en el estanque

**Estado:** Con pendientes — falta validar en la instancia 02
**Proyecto:** koi-garden
**Ticket:** KOI-1099

Buscar un pez por su código entre estanques distintos falla con timeout a los
30 segundos. El diagnóstico y el fix ya se validaron en
`koi-garden-pond-01`; falta repetir la validación en `koi-garden-pond-02`,
que es la instancia donde se reprodujo el incidente originalmente.

## Contenido

- `analysis/` — hipótesis de causa raíz.
- `benchmarks/` — línea base y corridas de latencia del lookup, en dos
  formatos: el propio de Engram (`engram.benchmark.v1`) y el formato nativo
  del harness de benchmarking, que se importa con un mapa de JSON Pointer.
- `evidences/` — traza del timeout capturada en producción (`01-timeout/`) y
  el log crudo del incidente (`02-timeout-log/`).
- `runbooks/` — pasos de mitigación mientras se valida el fix definitivo en
  pond-02 (ver también `Runbooks/network/RB-005-timeout-lookup.md`).

## Pendiente

Repetir la corrida de benchmarks (`benchmarks/run-bench.sh`) apuntando a
`koi-garden-pond-02` y confirmar que la p95 baja del mismo modo que en
pond-01 antes de poder cerrar este ticket.
