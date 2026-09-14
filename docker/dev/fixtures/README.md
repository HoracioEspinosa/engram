# Fixtures de desarrollo (todo ficticio)

Datos de prueba para el Engram dockerizado de `docker-compose.dev.yml`. Nada aquí es real: `koi-garden` y `tsukimi-bridge` son productos inventados, y los tickets `KOI-*`/`TSU-*` no existen fuera de este árbol.

- `vault/` — checkout ficticio de la base de conocimiento, montado en `/vault` (solo lectura).
- `vault/Runbooks/` en la raíz es la única forma que el escáner actual (`internal/runbooks/vault_scan.go`) recorre hoy.
- `vault/<proyecto>/<tarea>/<categoría>/` es la forma real del vault del usuario; el escáner actual solo recorre `vault/Runbooks/` (ver arriba), así que un archivo con esta estructura no se indexa.
- Los `service:` de los runbooks son deliberadamente ajenos a `internal/runbooks/service_map.go`. Tanto `runbooks sync --vault-dir` como `--entries-file` (`vault/Runbooks/.entries.json`) resuelven `service:` con ese mapa fijo, así que toda entrada del fixture sale `unknown_service` y `runbook_index` queda en 0 filas: esa es la línea base que debe llegar a `skipped = 0` cuando el mapa sea data-driven.
- `vault/evidence/` — raíz de compatibilidad para `CD_EVIDENCE_DIR` mientras siga apuntando fuera de `<tarea>/evidences/`.
- `mcp/session.jsonl` — sesión NDJSON de humo contra el vault sembrado (`--tools=agent,projects`).
- `mcp/reorg.jsonl` — sesión NDJSON del arnés de reorganización sobre la copia de la base real (`--tools=admin,projects`).
