#!/usr/bin/env bash
# Collect Datadog Agent diagnostics for Tagger/container ID issues
# Run on the host running the Datadog Agent (needs sudo)
set -euo pipefail
OUT_DIR="datadog_diagnostics_$(date +%Y%m%dT%H%M%SZ)"
mkdir -p "$OUT_DIR"

echo "Collecting datadog-agent status..."
if command -v datadog-agent >/dev/null 2>&1; then
  sudo datadog-agent status > "$OUT_DIR/agent_status.txt" 2>&1 || true
  sudo datadog-agent configcheck > "$OUT_DIR/agent_configcheck.txt" 2>&1 || true
else
  echo "datadog-agent binary not found in PATH; attempting journalctl logs instead" > "$OUT_DIR/agent_status.txt"
fi

echo "Collecting journal logs (last 1000 lines)..."
sudo journalctl -u datadog-agent -n 1000 --no-pager > "$OUT_DIR/journal.txt" 2>&1 || true

echo "Collecting agent log files..."
LOG_DIRS=(/var/log/datadog /var/log/datadog/agent)
for d in "${LOG_DIRS[@]}"; do
  if [ -d "$d" ]; then
    tar -czf "$OUT_DIR/agent_logs.tgz" -C "$d" . || true
    break
  fi
done

echo "Collecting grep for Tagger/OriginInfo errors..."
sudo grep -i "OriginInfo" -n /var/log/datadog/agent.log || true > "$OUT_DIR/origininfo_grep.txt" 2>&1 || true
sudo grep -i "unable to resolve container id" -n /var/log/datadog/agent.log || true > "$OUT_DIR/could_not_resolve_grep.txt" 2>&1 || true

echo "Checking runtime socket permissions..."
ls -l /var/run/docker.sock 2>/dev/null || true > "$OUT_DIR/docker_sock_stat.txt"
stat /var/run/docker.sock 2>/dev/null || true >> "$OUT_DIR/docker_sock_stat.txt" || true
ls -l /run/containerd/containerd.sock 2>/dev/null || true >> "$OUT_DIR/docker_sock_stat.txt" || true

if command -v kubectl >/dev/null 2>&1; then
  echo "Collecting Kubernetes node/pod info..." > "$OUT_DIR/k8s_overview.txt"
  kubectl get nodes -o wide >> "$OUT_DIR/k8s_overview.txt" 2>&1 || true
  kubectl get pods -A -o wide >> "$OUT_DIR/k8s_overview.txt" 2>&1 || true
fi

echo "Packaging diagnostics into tar.gz"
tar -czf "${OUT_DIR}.tgz" "$OUT_DIR" || true

echo "Diagnostics collected: ${OUT_DIR}.tgz"
ls -l "${OUT_DIR}.tgz" || true
