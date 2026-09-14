# Sample VHS tape: the initial screen in the koi-pond theme.
#
# `Set Theme` is deliberately omitted: the design calls for it to be
# generated from `engram theme export` once that command exists, at which
# point this tape gains a `Set Theme` block instead of relying on whatever
# palette the binary resolves by default.
#
# Run (profile `shots` of docker-compose.dev.yml, from the repository root):
#   docker compose -f docker-compose.dev.yml --profile shots run --rm vhs tapes/home-koi-pond.tape

Output /work/out/home-koi-pond.png

Set Width 1200
Set Height 700
Set FontSize 16
Set FontFamily "IosevkaTerm Nerd Font Mono"

Env ENGRAM_DATA_DIR /data
Env ENGRAM_VAULT_ROOT /vault
Env ENGRAM_TUI_ICONS nerd

Type "engram tui --project koi-garden"
Enter
Sleep 2s
Screenshot /work/out/home-koi-pond.png
