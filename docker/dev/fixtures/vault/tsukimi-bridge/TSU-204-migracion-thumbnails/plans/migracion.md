# Plan de migración — miniaturas a tsukimi-bridge

1. Migrar por lotes de 500 imágenes, empezando por el catálogo de
   `koi-garden-pond-01` (el de menor tráfico).
2. Comparar cada miniatura regenerada contra la original en tamaño y
   dimensiones, antes de descartar la miniatura vieja.
3. Pausar la migración si aparece más de un 1% de miniaturas corruptas en un
   mismo lote (ver `Runbooks/data-integrity/RB-004-thumbnails-corruptos.md`).
4. Repetir para `koi-garden` y `koi-garden-pond-02` una vez que el lote de
   pond-01 quede limpio.
