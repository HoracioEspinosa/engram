# Causa raíz — autologin con token expirado

## Resumen

El servicio de sesiones emitía el token de autologin con un TTL de **5
segundos** en vez de **5 minutos**. Cualquier usuario cuya app tardara más de
un par de segundos en llamar a `/api/autologin` después de cargar recibía un
token ya vencido y caía a la pantalla de acceso manual.

## Cómo se confirmó

1. Se capturó la respuesta cruda de `/api/autologin` en el momento de la
   falla (`../evidences/01-login-antes/respuesta-cruda.json`): `401
   invalid_token` con `token_ttl_seconds: 0`.
2. Se revisó el emisor del token y se encontró la constante de TTL con un
   valor de `5 * time.Second` en vez de `5 * time.Minute`.
3. Se reprodujo en local forzando una latencia artificial de 3 segundos entre
   la carga de la app y la llamada al endpoint: el bug se reprodujo de forma
   consistente.

## Por qué no se detectó antes

El entorno de desarrollo llama a `/api/autologin` casi instantáneamente
después de cargar, por lo que el TTL corto nunca alcanzaba a vencer ahí. Solo
se manifestaba con la latencia real de dispositivos de usuarios finales.
