# Plan de ataque — KOI-1042

1. Confirmar la causa raíz con la respuesta cruda del endpoint (hecho, ver
   `../analysis/causa-raiz.md`).
2. Corregir el TTL del token de autologin de 5 segundos a 5 minutos.
3. Agregar un mínimo de margen de reloj (skew) de 30 segundos para tolerar
   pequeñas diferencias entre el reloj del cliente y el del servidor.
4. Validar el fix en las tres instancias (`koi-garden`, `koi-garden-pond-01`,
   `koi-garden-pond-02`) antes de cerrar el ticket.
5. Dejar evidencia de antes/después y una regresión de QA sobre el listado
   principal, para descartar efectos colaterales del cambio de TTL.
