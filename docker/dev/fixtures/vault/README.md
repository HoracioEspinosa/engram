# Vault de conocimiento (ficticio)

Este árbol es una fixture de desarrollo. **Ningún proyecto, ticket, persona ni
incidente aquí es real** — `koi-garden` y `tsukimi-bridge` son productos
inventados para validar, en un Engram dockerizado y aislado, la forma real en
la que el usuario organiza su conocimiento: `<proyecto>/<tarea>/<categoría>`.

Este README imita el README raíz real del usuario: una tabla "Mapa de tareas"
que el importador de vault (fase 2) debe aprender a parsear para crear
proyectos y tareas automáticamente.

## Mapa de tareas

| Tarea | Qué es | Estado | Archivos |
| --- | --- | --- | --- |
| [Hardening del autologin](./koi-garden/KOI-1042-hardening-autologin/README.md) | Endurecer el autologin tras el incidente de tokens vencidos | Cerrado (2026-08-14) | 11 |
| [Timeout de lookup en el estanque](./koi-garden/KOI-1099-lookup-timeout/README.md) | Investigar y mitigar el timeout al buscar peces entre estanques | Con pendientes | 12 |
| [División del filesharing del estanque](./koi-garden/split-pond-filesharing/README.md) | Evaluar separar el filesharing de koi-garden en un servicio propio | Sin confirmar | 3 |
| [Mantenimiento del estanque](./koi-garden/mantenimiento-del-estanque/README.md) | Bitácora de tareas puntuales que no ameritan una carpeta propia | Histórico | 3 |
| [Migración de miniaturas](./tsukimi-bridge/TSU-204-migracion-thumbnails/README.md) | Migrar la generación de miniaturas de la galería al nuevo pipeline | Con pendientes | 6 |

## Proyectos

- [`koi-garden/`](./koi-garden/README.md) — panel y API de un estanque de peces
  koi (ficticio), con tres instancias.
- [`tsukimi-bridge/`](./tsukimi-bridge/README.md) — servicio ficticio que
  conecta la galería de koi-garden con su almacenamiento de miniaturas.

## Dos formas, a propósito

`Runbooks/` en esta misma raíz es la única forma que el escáner de runbooks
actual (`internal/runbooks/vault_scan.go`) recorre. `<proyecto>/<tarea>/<categoría>/`
es la forma real en la que vive `~/.clarodrive`, y es la que el importador de
vault de la fase 2 tiene que aprender a leer. Esta carpeta hace convivir ambas
formas para que ese hueco sea un caso de prueba, no una sorpresa.
