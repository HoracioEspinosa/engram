# Fixtures de desarrollo (todo ficticio)

Datos de prueba para el Engram dockerizado de `docker-compose.dev.yml`. Nada aquí es real: `koi-garden` y `tsukimi-bridge` son productos inventados, y los tickets `KOI-*`/`TSU-*` no existen fuera de este árbol.

- `vault/` — checkout ficticio de la base de conocimiento, montado en `/vault` (solo lectura).
- `vault/Runbooks/` en la raíz es la única forma que el escáner actual (`internal/runbooks/vault_scan.go`) recorre hoy.
- `vault/<proyecto>/<tarea>/<categoría>/` es la forma real del vault del usuario, que el importador de la fase 2 debe aprender a leer.
- Los `service:` de los runbooks son deliberadamente ajenos a `internal/runbooks/service_map.go`: `runbooks sync --vault-dir` mide una línea base de `unknown_service`, mientras que `--entries-file` (`vault/Runbooks/.entries.json`) no pasa por ese mapa.
- `vault/evidence/` — raíz de compatibilidad para `CD_EVIDENCE_DIR` mientras siga apuntando fuera de `<tarea>/evidences/`.
- `mcp/session.jsonl` — sesión NDJSON de humo contra el vault sembrado (`--tools=agent,projects`).
- `mcp/reorg.jsonl` — sesión NDJSON del arnés de reorganización sobre la copia de la base real (`--tools=admin,projects`).
