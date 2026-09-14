# Pasos de mitigación mientras se valida el fix en pond-02

Copia local, a nivel de tarea, de los pasos de
`Runbooks/network/RB-005-timeout-lookup.md`, para que quien retome KOI-1099
no tenga que salir de la carpeta de la tarea:

1. Confirmar que el timeout explícito del cliente de lookup
   (`benchmarks/instrumentation.patch`) está desplegado en la instancia que
   falla.
2. Si el timeout está desplegado y el síntoma persiste, escalar como
   incidente de red del estanque destino, no como bug del cliente.
3. Mientras no se confirme el fix en pond-02, avisar a soporte que el
   síntoma conocido es "timeout a los 30 segundos buscando entre estanques"
   y que el runbook RB-005 tiene la mitigación manual.
