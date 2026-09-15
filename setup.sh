#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILLS_SOURCE="${REPO_ROOT}/skills"
AGENTS_SOURCE="${REPO_ROOT}/agents"

if [ ! -d "${SKILLS_SOURCE}" ]; then
  echo "skills directory not found at ${SKILLS_SOURCE}" >&2
  exit 1
fi

link_skills() {
  local agent_dir="$1"
  local target_dir="${agent_dir}/skills"
  local source_path=""
  local skill_name=""
  local link_path=""

  mkdir -p "${target_dir}"

  # Remove legacy aggregate link if present.
  if [ -L "${target_dir}/engram" ]; then
    rm -f "${target_dir}/engram"
  fi

  for source_path in "${SKILLS_SOURCE}"/*; do
    skill_name="$(basename "${source_path}")"
    link_path="${target_dir}/${skill_name}"
    ln -sfn "${source_path}" "${link_path}"
    echo "linked ${link_path} -> ${source_path}"
  done
}

# Agents are copied, not linked. Skills resolve through a symlinked directory
# fine, but agent discovery under .claude/agents/ is only documented for real
# files, and a definition that silently resolves to nothing produces an agent
# that is simply absent rather than an error. A copy is the shape that is
# known to work.
copy_agents() {
  local agent_dir="$1"
  local target_dir="${agent_dir}/agents"
  local source_path=""
  local agent_name=""

  if [ ! -d "${AGENTS_SOURCE}" ]; then
    return 0
  fi

  mkdir -p "${target_dir}"

  for source_path in "${AGENTS_SOURCE}"/*.md; do
    [ -e "${source_path}" ] || continue
    agent_name="$(basename "${source_path}")"
    cp -f "${source_path}" "${target_dir}/${agent_name}"
    echo "copied ${target_dir}/${agent_name} <- ${source_path}"
  done
}

link_skills "${REPO_ROOT}/.claude"
link_skills "${REPO_ROOT}/.codex"
link_skills "${REPO_ROOT}/.gemini"

copy_agents "${REPO_ROOT}/.claude"

echo
echo "Done. Skills linked for project .claude, .codex, and .gemini; agents copied to .claude/agents"
