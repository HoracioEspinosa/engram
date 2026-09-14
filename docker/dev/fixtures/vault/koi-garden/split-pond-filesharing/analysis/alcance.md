# Borrador de alcance — split del filesharing

## Motivación

`tsukimi-bridge` ya separó la generación de miniaturas en un servicio propio.
Cabe preguntarse si el filesharing de fotos entre estanques debería seguir el
mismo camino.

## Opciones consideradas

1. **Dejarlo como está.** El filesharing actual funciona; separarlo es
   trabajo sin un problema concreto que lo justifique todavía.
2. **Extraer un servicio `pond-filesharing`.** Mismo patrón que
   `tsukimi-bridge`: API propia, misma base de datos por ahora.
3. **Delegarlo por completo a `tsukimi-bridge`.** Fusionar filesharing y
   miniaturas en un solo servicio de medios.

## Estado

Sin confirmar. Esta nota es un punto de partida para la conversación, no una
decisión. La prueba de concepto en `../patches/split-filesharing.patch` es
solo para tener algo concreto que discutir, no algo listo para aplicar.
