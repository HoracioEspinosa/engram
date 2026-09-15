# Línea base — lookup-timeout (KOI-1099)

## Metodología

`run-bench.sh` dispara 200 lookups cruzados entre `koi-garden-pond-02` y
`koi-garden-pond-01`, mide la latencia por intento y agrega p95 y conteo de
errores. Cada corrida escribe un `baseline-runN.json`.

## Corridas

| Corrida | Config | p95 lookup | Errores | Baseline |
| --- | --- | --- | --- | --- |
| `baseline-run1.json` | pond-02 / cache off | 1512 ms | 3 | sí |
| `baseline-run2.json` | pond-02 / cache on | 980 ms | 1 | no |
| `baseline-run3.json` | harness externo (formato ajeno, ver `benchmark_map.json`) | ver `wallClockMs` | — | no |

## Lectura

`baseline-run1.json` es la corrida base tomada antes de instrumentar el
timeout explícito del cliente de lookup
(`instrumentation.patch`). `baseline-run2.json` repite la misma prueba con la
caché de resolución de estanques activada, ya con el timeout explícito
aplicado: la p95 baja de forma consistente. `baseline-run3.json` es la salida
cruda de un harness externo que no habla el formato `engram.benchmark.v1`; se
importa con el mapa de JSON Pointer de `benchmark_map.json`.
