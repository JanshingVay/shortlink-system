#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/../deployments/.env"

if [ -f "$ENV_FILE" ]; then
  export $(grep -v '^#' "$ENV_FILE" | xargs)
fi

REDIS_PASSWORD=${REDIS_PASSWORD:-redis_secure_password_789!}

echo "等待 Redis 节点启动..."
sleep 20

echo "创建 Redis 集群..."
docker run --rm --network deployments_shortlink-network redis:7.0-alpine redis-cli --cluster create \
  redis-node-1:7001 \
  redis-node-2:7002 \
  redis-node-3:7003 \
  redis-node-4:7004 \
  redis-node-5:7005 \
  redis-node-6:7006 \
  --cluster-replicas 1 \
  --cluster-yes \
  -a "${REDIS_PASSWORD}"

echo "Redis 集群创建完成！"
