# Hipótesis — timeout de lookup entre estanques

## Síntoma

Buscar un pez por su código cuando el lookup cruza de `koi-garden-pond-02` a
otro estanque termina en timeout a los 30 segundos. Los reintentos
automáticos no mejoran la tasa de éxito.

## Hipótesis

El cliente de lookup (`LookupClient.Resolve`) no tenía un timeout propio
configurado: heredaba el límite por defecto del transporte HTTP subyacente,
mucho más alto que lo tolerable para una búsqueda interactiva. Cuando el
estanque destino estaba lento o inalcanzable, la llamada se quedaba
esperando hasta el límite del transporte en vez de fallar rápido.

## Cómo se va a confirmar

1. Instrumentar el cliente con temporización explícita por intento
   (`benchmarks/instrumentation.patch`).
2. Capturar una línea base de latencia contra pond-01
   (`benchmarks/baseline-run1.json`, `baseline-run2.json`).
3. Repetir la misma corrida contra pond-02 y comparar.

## Estado

Confirmada en pond-01. Pendiente de repetir en pond-02 antes de cerrar el
ticket — ver `README.md`.
