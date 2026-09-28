#!/usr/bin/env bash
# scripts/start_cluster.sh — 一键启动三节点 MedTrust-Raft 演示集群
# 用法：bash scripts/start_cluster.sh
# 停止：bash scripts/start_cluster.sh stop

set -e

BINARY="./medtrust-node"
LOG_DIR="./logs"
PID_FILE="$LOG_DIR/cluster.pids"

build() {
  echo "[BUILD] compiling..."
  go build -o "$BINARY" ./cmd/node/
  echo "[BUILD] done → $BINARY"
}

start() {
  mkdir -p "$LOG_DIR"
  build

  pids=()
  for i in 1 2 3; do
    cfg="configs/node${i}.yaml"
    log="$LOG_DIR/node${i}.log"
    echo "[START] node-${i}  config=$cfg  log=$log"
    "$BINARY" --config "$cfg" > "$log" 2>&1 &
    pids+=($!)
  done

  echo "${pids[*]}" > "$PID_FILE"
  echo ""
  echo "Cluster started. PIDs: ${pids[*]}"
  echo "Logs : $LOG_DIR/node{1,2,3}.log"
  echo "APIs : http://127.0.0.1:8001  8002  8003"
  echo ""
  echo "Quick test:"
  echo "  curl -s http://127.0.0.1:8001/api/node/status | python3 -m json.tool"
  echo "  curl -s -X POST http://127.0.0.1:8001/api/record/upload \\"
  echo "       -H 'Content-Type: application/json' \\"
  echo "       -d '{\"payload\":\"EncryptedRecord-001\"}'"
  echo "  curl -s http://127.0.0.1:8002/api/records | python3 -m json.tool"
}

stop() {
  if [ ! -f "$PID_FILE" ]; then
    echo "No PID file found at $PID_FILE"
    exit 0
  fi
  read -ra pids < "$PID_FILE"
  for pid in "${pids[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" && echo "[STOP] killed PID $pid"
    fi
  done
  rm -f "$PID_FILE"
  echo "Cluster stopped."
}

case "${1:-start}" in
  start) start ;;
  stop)  stop  ;;
  *)
    echo "Usage: $0 [start|stop]"
    exit 1
    ;;
esac
