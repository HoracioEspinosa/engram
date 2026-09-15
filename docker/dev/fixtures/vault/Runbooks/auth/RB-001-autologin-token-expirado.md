---
type: runbook
id: RB-001
title: Autologin falla con token expirado
service: koi-garden
category: auth
pattern: auth-access
severity: P2
status: verified
symptoms:
  - El autologin regresa a la pantalla de acceso en vez de entrar directo al estanque
  - La respuesta de /api/autologin devuelve 401 invalid_token
  - El síntoma aparece siempre unos segundos después de abrir la aplicación
owner: equipo-koi-garden
automation_level: assisted
last_updated: 2026-08-14
last_verified: 2026-09-01
---

# Autologin falla con token expirado

## Síntomas

Los usuarios que abren koi-garden con la sesión recordada caen en la pantalla de
login en vez de entrar directo al panel del estanque. El navegador muestra una
llamada a `/api/autologin` que responde `401 invalid_token` un par de segundos
después de cargar la app.

## Diagnóstico

El token de autologin se emitía con un TTL de 5 segundos en vez de 5 minutos:
cualquier usuario con más de un par de segundos de latencia entre la carga de
la app y la llamada al endpoint recibía un token ya vencido. El detalle
completo de la causa raíz vive en
`koi-garden/KOI-1042-hardening-autologin/analysis/causa-raiz.md`.

El emisor queda así una vez corregido el TTL:

```go
// autologinTTL is how long a remembered-session token stays valid.
const autologinTTL = 5 * time.Minute

func issueAutologinToken(userID string, now time.Time) (Token, error) {
	if userID == "" {
		return Token{}, errors.New("autologin: empty user id")
	}
	return Token{Subject: userID, ExpiresAt: now.Add(autologinTTL)}, nil
}
```

## Pasos de mitigación

1. Confirmar el TTL configurado del token de autologin en el servicio de sesiones.
2. Si el TTL es menor a 1 minuto, aplicar `patches/autologin-token-ttl.patch` de
   la tarea KOI-1042 (o su equivalente ya mergeado).
3. Repetir el login con la sesión recordada y confirmar que entra sin pasar por
   la pantalla de acceso.

## Verificación

Revisar semanalmente el panel de tasa de error de `/api/autologin`: un valor
por encima de 0.5% durante más de 10 minutos reabre este runbook.
