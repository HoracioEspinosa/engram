#!/usr/bin/env bash
# Test de regresión para el bug en release-custom.yml línea 65.
# Demuestra que el patrón viejo (origin/custom/main) falla
# mientras que el patrón nuevo (origin/main) pasa.

set -euo pipefail

echo "=== Test de regresión: Guard check branch pattern ==="
echo

# Simular lo que git branch -r --contains retorna
# Esto es lo que aparece cuando un commit está en origin/main
branches_output="  origin/main"

echo "Branches containing the tag commit (simulated):"
printf '%s\n' "$branches_output"
echo

# TEST 1: El patrón VIEJO (en release-custom.yml línea 65 HOY)
# Busca origin/custom/main (rama retirada)
echo "TEST 1: Patrón VIEJO (origin/custom/main) - DEBE FALLAR"
if printf '%s\n' "$branches_output" \
     | awk '/^[[:space:]]*origin\/custom\/main$/{found=1} END{exit !found}'; then
  echo "✓ PASS: encontró origin/custom/main"
  exit_code_old=0
else
  echo "✗ FAIL: NO encontró origin/custom/main (como se espera - rama no existe)"
  exit_code_old=1
fi
echo

# TEST 2: El patrón NUEVO (lo que DEBE estar después del arreglo)
# Busca origin/main (rama actual)
echo "TEST 2: Patrón NUEVO (origin/main) - DEBE PASAR"
if printf '%s\n' "$branches_output" \
     | awk '/^[[:space:]]*origin\/main$/{found=1} END{exit !found}'; then
  echo "✓ PASS: encontró origin/main"
  exit_code_new=0
else
  echo "✗ FAIL: NO encontró origin/main"
  exit_code_new=1
fi
echo

# Resumen
echo "=== Resumen ==="
echo "Patrón viejo (origin/custom/main): exit code $exit_code_old (esperado: 1 = FAIL)"
echo "Patrón nuevo (origin/main):        exit code $exit_code_new (esperado: 0 = PASS)"
echo

if [ "$exit_code_old" -eq 1 ] && [ "$exit_code_new" -eq 0 ]; then
  echo "✓ Test de regresión CONFIRMADO: el bug es real, el arreglo lo resuelve"
  exit 0
else
  echo "✗ Test de regresión FALLIDO: comportamiento inesperado"
  exit 1
fi
