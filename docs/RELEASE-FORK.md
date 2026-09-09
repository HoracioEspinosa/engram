# Release del fork de engram para ClaroDrive

Este documento es el procedimiento completo para publicar una versión del fork
`HoracioEspinosa/engram` y actualizar con ella las computadoras del equipo y el
runtime de cloud.

El fork tiene un solo remoto, `origin`, que apunta al fork. **Nunca se escribe
en `Gentleman-Programming/engram`**: el upstream se lee y nada más.

## 1. Qué se publica

| Artefacto | Dónde vive | Quién lo produce |
|---|---|---|
| Binarios `darwin`/`linux`/`windows`, `amd64`/`arm64` | GitHub Release de `HoracioEspinosa/engram` | `.goreleaser.custom.yaml` |
| Fórmula de Homebrew `engram-custom` (macOS y Linux) | `HoracioEspinosa/homebrew-tap`, carpeta `Formula/` | `.goreleaser.custom.yaml` |
| Imagen de contenedor | `ghcr.io/horacioespinosa/engram:custom`, `:<versión>`, `:sha-<commit>` | `docker/custom/Dockerfile` |
| Imagen del canal edge | `ghcr.io/horacioespinosa/engram:edge`, `:edge-<commit>` | `publish-cloud-image.yml`, en cada push a `main` |

La fórmula usa `brews`, el tipo de artefacto que GoReleaser marcó obsoleto en favor de
`homebrew_casks` para binarios precompilados — pero obsoleto no es lo mismo que retirado:
`goreleaser check` lo reporta con salida 2 («usa propiedades obsoletas»), nunca con salida 1
(«configuración inválida»), y sigue generando una fórmula correcta. Se eligió sobre el cask porque
Homebrew solo sirve casks en macOS, y este equipo necesita `brew install` en Linux también. El paso
de validación de `release-custom.yml` distingue ambos códigos explícitamente: la salida 2 pasa con
un aviso, la 1 (o cualquier otra) sigue siendo fatal.

La rama de release es `main`. La etiqueta tiene la forma
`v<versión-de-upstream>-cd.<n>`, por ejemplo `v1.20.0-cd.1`: la base dice sobre
qué versión de upstream se construyó y el sufijo cuenta las revisiones propias.

Esa forma no es cosmética. Los workflows de upstream (`release.yml` y
`cloud-image.yml`) se disparan con etiquetas `v*` y publican en el tap y en el
namespace de imágenes de upstream. Ambos quedaron limitados al repositorio de
upstream con `if: github.repository == 'Gentleman-Programming/engram'`, así que
una etiqueta puesta en el fork ya no los enciende. `release-custom.yml` es el
único que corre aquí, y solo con `v*-cd.*`.

## 2. Preparación, una sola vez

Estos tres pasos son acciones del usuario, no del pipeline.

### 2.1 Crear el tap

El repositorio `HoracioEspinosa/homebrew-tap` **todavía no existe**
(comprobado con `git ls-remote`, que responde 128). Sin él, la fórmula no se
puede publicar; el resto del release sí funciona, porque el workflow detecta
la ausencia del token y se salta ese paso.

```
gh repo create HoracioEspinosa/homebrew-tap --public \
  --description "Homebrew tap de ClaroDrive"
```

El nombre del repositorio tiene que ser exactamente `homebrew-tap`: es lo que
convierte `brew tap horacioespinosa/tap` en una ruta válida.

### 2.2 Crear el token del tap

GoReleaser escribe la fórmula en un repositorio distinto del que corre la acción,
así que el `GITHUB_TOKEN` del workflow no alcanza. Hace falta un token de acceso
personal con permiso de escritura de contenido **solo** sobre
`HoracioEspinosa/homebrew-tap`, guardado como secreto del repositorio del fork
con el nombre `HOMEBREW_TAP_TOKEN`.

```
gh secret set HOMEBREW_TAP_TOKEN --repo HoracioEspinosa/engram
```

Si el secreto no está, el release sigue adelante y publica binarios e imagen; el
resumen del workflow dice `HOMEBREW_TAP_TOKEN is unset, formula NOT published`.

### 2.3 Habilitar Actions y paquetes en el fork

En un fork, GitHub deja los workflows deshabilitados hasta que alguien los
habilita a mano, y el primer `docker push` a `ghcr.io` crea el paquete como
privado. Después del primer release hay que darle visibilidad al paquete si el
host de cloud lo va a bajar sin credenciales.

## 3. Ciclo de release

### 3.1 Traer los cambios de upstream

`main` es a la vez la rama de integración del fork y la de release, así que
lleva los commits propios encima del código de upstream. Eso significa que
**no se puede sincronizar con `gh repo sync`**: ese comando espera una rama sin
commits propios y aquí hay bastantes. Tampoco aplica de otra forma — este
repositorio es un derivado independiente, no un fork de GitHub.

La vía es el rebase de la sección siguiente, que clona a un directorio
temporal y añade el remoto de upstream en modo lectura. Nada de lo que hagas
aquí escribe en `Gentleman-Programming/engram`.

### 3.2 Rebase de `main` sobre upstream

**Antes del rebase:** asegúrate de que todos los commits que quieren viajar en
el release estén en `main`. Si hay una rama de feature sin fusionar, el release
no la va a incluir por muchos commits que tenga. Fusionarla es un paso manual:

```
git -C ~/Projects/engram checkout main
git -C ~/Projects/engram merge --ff-only <rama>
git -C ~/Projects/engram push origin main
```

Luego, sí, el rebase:

```
scripts/release/rebase-onto-upstream.sh
```

El script no toca tu copia de trabajo: clona el repositorio a un directorio
temporal, agrega el remoto de upstream en modo lectura, deja ambos remotos del
clon con una URL de push inválida, y hace ahí el rebase. Códigos de salida:

| Código | Significado |
|---|---|
| 0 | Rebase limpio y `go build` + `go test` en verde |
| 1 | Error de uso, o falta algo para poder comprobar |
| 2 | El rebase se detuvo en un conflicto; imprime el inventario y conserva el directorio |
| 3 | El rebase quedó limpio pero el árbol no compila o no pasa sus pruebas |

Con `--adopt <rama>` crea la rama resultante dentro de tu repositorio sin mover
`HEAD` y sin hacer push. Publicarla sigue siendo un acto manual:

```
git -C ~/Projects/engram push origin <rama>:main
```

`rerere` queda activado dentro del clon, así que una resolución hecha una vez se
vuelve a aplicar sola en el siguiente intento sobre ese mismo clon.

#### Inventario de conflictos medido

El rebase de los 11 commits propios sobre `upstream/main` se ejecutó completo en
un clon desechable. Se detiene tres veces:

| Commit | Archivos en conflicto | Bloques |
|---|---|---|
| `9d6db6c` doctor | 1 (`internal/diagnostic/diagnostic_test.go`) | 2 |
| `e934a5d` perfil MCP `projects` | 2 (`cmd/engram/main.go`, `internal/mcp/mcp_test.go`) | 4 |
| `0e0c780` HTTP + CLI + sync + runbooks | 11 UU + 2 UD | 16 |

**Nota sobre el recuento:** La cantidad de archivos y bloques en conflicto depende
de cómo se hayan resuelto los conflictos anteriores en la secuencia. Los dos
primeros (doctor y perfil) son estables y reproducibles. El tercero varía: el
número de archivos y bloques listados es de una corrida con una estrategia de
resolución específica (quedarse con el lado replayeado en cada conflicto
anterior). Un operador que resuelva los conflictos de otra manera puede obtener
un recuento distinto en esta parada. Los commit hashes y las paradas son
invariantes.

Los dos primeros son mecánicos y siempre iguales: upstream agrega checks al
registro de diagnóstico y el commit propio agrega el suyo, de modo que la
resolución es la unión ordenada por el valor del código; y upstream reemplazó
las cuentas de herramientas escritas a mano por una comparación de inventario,
que ya tolera las 10 herramientas del perfil `projects`.

El tercero es caro y va a seguir siéndolo: ese commit trae, además del trabajo
de `engram-projects`, una reestructuración completa de la TUI —99 archivos,
17,795 líneas agregadas— que mueve `internal/tui/model.go`, `update.go`,
`clipboard_test.go` y compañía a `internal/tui/tabs/memory/` y borra
`internal/tui/view.go`. Upstream sigue editando esos archivos en su ubicación
vieja, así que cada rebase paga un conflicto de renombrado contra modificación
en siete archivos de la TUI. Separar esa reestructuración en su propio commit
—o llevarla a upstream— es lo que abarata los rebases siguientes.

### 3.3 Ensayo local antes de etiquetar

```
scripts/release/build-release.sh --tag v1.20.0-cd.1 --image
```

Construye todo en un clon temporal, nunca en tu copia de trabajo, y **no publica
nada**: cada invocación de goreleaser lleva `--skip=publish,announce` y no hay
manera de pasarle argumentos extra. Al final ejecuta el binario recién
construido y falla si `engram --version` no dice exactamente la versión de la
etiqueta. Con `--image` hace lo mismo con la imagen.

#### Inspeccionar la fórmula generada sin publicarla

`--skip=publish` se salta la fórmula entera, no solo el push: GoReleaser
escribe el `.rb` como parte de la misma etapa de `publish`. Para ver el
contenido real sin necesitar el token del tap ni tocar GitHub, se agrega
`skip_upload: true` a una **copia descartable** del `brews` de
`.goreleaser.custom.yaml` (nunca al archivo real, porque eso apagaría la
publicación de verdad) y se corre `goreleaser release` contra esa copia dentro
del propio clon temporal que crea `build-release.sh --keep`. Con
`skip_upload: true`, GoReleaser escribe la fórmula en
`dist/homebrew/Formula/engram-custom.rb` en vez de comitearla, que es lo que
se usó para verificar esta fila.

#### Alcance compartido del `.dockerignore`

El archivo `.dockerignore` en la raíz afecta a TODAS las construcciones de imagen
del repositorio, no solo a `docker/custom/Dockerfile`. También se aplica a
`docker/cloud/Dockerfile`, que usan los workflows `publish-cloud-image.yml` y
`cloud-image.yml`. Los patrones de `.dockerignore` están diseñados para no
colisionar con los `go:embed` del proyecto (internal/setup/plugins/opencode,
internal/tasks/jira_state_map.json, internal/obsidian/graph.json,
internal/cloud/dashboard/static), de modo que ambas construcciones siguen
funcionando. Si algún futuro cambio extiende los patrones de `.dockerignore`,
verifica que siga siendo compatible con ambos Dockerfiles.

Las pruebas de los scripts viven en `scripts/release/tests/run.sh` y corren la
batería completa bajo el bash 3.2 de macOS y bajo bash 5.

### 3.4 Etiquetar y publicar

```
git -C ~/Projects/engram tag -a v1.20.0-cd.1 -m "engram 1.20.0-cd.1 (fork ClaroDrive)" main
git -C ~/Projects/engram push origin v1.20.0-cd.1
```

El push de la etiqueta enciende `release-custom.yml`, que:

1. Comprueba la forma de la etiqueta y que el commit esté en `origin/main`.
2. Corre `go test ./...`. Hace falta porque `ci.yml` solo corre en `main` y en
   pull requests: `main` llega a la etiqueta sin haber pasado por CI.
3. Publica binarios y, si hay token, la fórmula.
4. Construye y sube la imagen para `linux/amd64` y `linux/arm64`, y después la
   **vuelve a bajar por digest** y verifica que adentro `engram --version`
   responda la versión de la etiqueta. Si no coincide, el release queda rojo.
5. Escribe en el resumen del workflow el digest de la imagen, que es lo que se
   usa para desplegar y para revertir.

## 4. Instalación en las computadoras del equipo

### macOS y Linux

```
brew tap horacioespinosa/tap
brew install engram-custom
engram --version
engram doctor
```

Si la máquina ya tiene el `engram` de upstream instalado con `brew`, la
instalación se niega porque ese binario ya ocupa
`$(brew --prefix)/bin/engram`. Se desinstala primero y se vuelve a intentar:

```
brew uninstall engram
```

La fórmula se llama `engram-custom` justamente para que `brew upgrade engram`
no pueda devolver una máquina al binario de upstream sin que su dueño se
entere. El binario que instala sigue llamándose `engram`.

**Si la máquina tiene instalado el cask anterior** (`HoracioEspinosa/homebrew-tap`
publicó `engram-custom` como cask antes de esta fila; el cask y la fórmula no
pueden convivir bajo el mismo nombre), se desinstala primero:

```
brew uninstall --cask engram-custom
brew install engram-custom
```

### Windows

Homebrew no cubre Windows. Se baja el comprimido del release y se verifica
contra `checksums.txt`:

```
gh release download v1.20.0-cd.1 --repo HoracioEspinosa/engram \
  --pattern 'engram_1.20.0-cd.1_windows_amd64.zip' --pattern 'checksums.txt'
```

Ver [docs/INSTALLATION.md](../docs/INSTALLATION.md#windows) para el resto del
procedimiento en Windows.

## 5. Actualizar el cloud sin perder datos

El runtime de cloud guarda todo en PostgreSQL, no en la imagen. La imagen es
reemplazable; la base no.

### 5.1 Cómo se aplica el esquema, y qué implica

`CloudStore.migrate` corre al arrancar y ejecuta DDL idempotente: 11
`CREATE TABLE IF NOT EXISTS` y una lista de `ALTER TABLE ... IF NOT EXISTS`. No
hay tabla de versiones de esquema ni migraciones de vuelta, y tres de esas
sentencias son destructivas en el sentido de que cambian el tipo de una columna
o quitan una restricción.

Consecuencia práctica: **volver a la imagen anterior no deshace el esquema**. El
plan de reversión no puede ser "vuelvo a desplegar la imagen vieja" a secas;
tiene que incluir el respaldo.

### 5.2 Respaldo, obligatorio antes de desplegar

```
pg_dump --format=custom --no-owner \
  --dbname "$ENGRAM_DATABASE_URL" \
  --file "engram-cloud-$(date +%Y%m%d%H%M%S).dump"
```

Guarda el archivo fuera del host que estás por actualizar y anota su tamaño. Un
respaldo que nadie miró no es un respaldo.

### 5.3 Verificar la imagen antes de moverle nada al servicio

Toma el digest del resumen del workflow y compruébalo:

```
docker pull ghcr.io/horacioespinosa/engram@sha256:<digest>
docker run --rm ghcr.io/horacioespinosa/engram@sha256:<digest> --version
docker image inspect ghcr.io/horacioespinosa/engram@sha256:<digest> \
  --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
```

La primera línea tiene que responder `engram <versión>` y la tercera, el commit
de `main` del que salió.

### 5.4 Desplegar por digest

Configura el servicio con `ghcr.io/horacioespinosa/engram@sha256:<digest>`, no
con `:custom`. La etiqueta que se mueve sirve para saber cuál es la última; el
digest es lo único que permite decir sin ambigüedad a qué se está volviendo si
algo sale mal.

La imagen trae `HEALTHCHECK` contra `/health`, que responde 200 recién cuando el
esquema terminó de migrar. Un contenedor que no llega a `healthy` significa
"todavía no le mandes tráfico", no "el proceso se cayó".

Verificación después del despliegue:

```
curl -fsS https://<host-del-cloud>/health
```

### 5.5 Revertir

1. Volver a apuntar el servicio al digest anterior.
2. Si el esquema cambió, restaurar el respaldo de 5.2:

```
pg_restore --clean --if-exists --no-owner \
  --dbname "$ENGRAM_DATABASE_URL" engram-cloud-<marca-de-tiempo>.dump
```

3. Volver a comprobar `/health` y correr `engram doctor` desde una máquina
   cliente contra el cloud.

## 6. Límites conocidos

- **El aviso de actualización consulta este fork por defecto, no upstream, pero
  hoy consulta un endpoint que 404 siempre.** `internal/version.repoOwner`/`repoName`
  son variables, no constantes, con default `HoracioEspinosa/engram`:
  `internal/version` consulta `api.github.com/repos/HoracioEspinosa/engram/releases/latest`,
  que excluye prelanzamientos por diseño de la API de GitHub, y las cuatro
  etiquetas publicadas del fork están marcadas prerelease — así que la
  comprobación nunca puede acertar. ADR-045 §2 documenta la causa y la
  decisión (pasar al endpoint de lista); esa fila sigue abierta, sin tocar.
  El comando que este aviso sugiere en macOS y Linux es
  `brew update && brew upgrade HoracioEspinosa/tap/engram-custom`, sin
  `--cask` — la fórmula es el único artefacto del tap. Un downstream que
  necesite apuntar a otro remoto sobreescribe `repoOwner`/`repoName` en el
  enlace con `-ldflags "-X <module>/internal/version.repoOwner=... -X
  <module>/internal/version.repoName=..."`, sin tocar el código.
- **Los binarios están firmados ad hoc, no notarizados.** Una fórmula de
  Homebrew instala vía `curl`, que no aplica el atributo de cuarentena de
  Gatekeeper — solo lo hacen los navegadores y Homebrew Cask, que lo añade a
  propósito como medida propia (`Homebrew/homebrew-cask#22388`). Así que
  `brew install engram-custom` no necesita limpiarlo. Si alguien baja el
  comprimido a mano por un camino que sí lo aplica (un navegador, por
  ejemplo), tiene que hacerlo él:
  `xattr -dr com.apple.quarantine ./engram`.
- **`main` mezcla dos trabajos.** El commit `0e0c780` trae la
  reestructuración de la TUI junto con `engram-projects`. Es lo que hace caro
  cada rebase; ver el inventario de 3.2.
