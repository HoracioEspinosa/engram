# Log crudo del timeout de lookup entre pond-02 y pond-01

Este log demuestra, con marcas de tiempo, que la búsqueda del pez
`KOI-PZ-0917` desde `pond-02` hacia `pond-01` agota el deadline de 30
segundos en el intento inicial y en los dos reintentos siguientes, hasta que
el lookup se abandona por completo. Es la evidencia cruda que respalda la
traza capturada en `01-timeout/01-traza.png`.
