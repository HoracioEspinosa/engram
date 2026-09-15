# koi-garden (ficticio)

Panel y API de administración de un estanque de peces koi. Proyecto
inventado para las fixtures de desarrollo de Engram — no representa ningún
producto real.

## Instancias

`koi-garden` replica el patrón de instancias duplicadas del mismo repositorio
que el usuario usa en su entorno real (`nextcloud_00/01/02`):

| Instancia | Rol | Rama |
| --- | --- | --- |
| `koi-garden` | Instancia base / producción | `main` |
| `koi-garden-pond-01` | Réplica de referencia, mismo commit que la base | `main` |
| `koi-garden-pond-02` | Instancia de trabajo, donde se reproducen y validan los incidentes activos | `feature/koi-1099-lookup-timeout` |

## Tareas

| Tarea | Estado |
| --- | --- |
| [KOI-1042-hardening-autologin](./KOI-1042-hardening-autologin/README.md) | Cerrado (2026-08-14) |
| [KOI-1099-lookup-timeout](./KOI-1099-lookup-timeout/README.md) | Con pendientes |
| [split-pond-filesharing](./split-pond-filesharing/README.md) | Sin confirmar |
| [mantenimiento-del-estanque](./mantenimiento-del-estanque/README.md) | Histórico |
