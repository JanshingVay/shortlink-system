#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="${SCRIPT_DIR}/../deployments"

echo "========================================="
echo "   短链接系统 - 一键部署"
echo "========================================="

cd "$DEPLOY_DIR"

if [ ! -f .env ]; then
  echo "警告: .env 文件不存在，将使用默认配置"
  echo "建议编辑 deployments/.env 修改密码"
fi

echo ""
echo "1. 启动所有服务..."
docker-compose up -d

echo ""
echo "2. 等待服务启动（30秒）..."
sleep 30

echo ""
echo "3. 初始化 MySQL 主从复制..."
cd "$SCRIPT_DIR"
chmod +x init-mysql-replication.sh
./init-mysql-replication.sh

echo ""
echo "4. 初始化 Redis 集群..."
chmod +x init-redis-cluster.sh
./init-redis-cluster.sh

echo ""
echo "========================================="
echo "   部署完成！"
echo "========================================="
echo ""
echo "访问地址: http://localhost"
echo ""
echo "查看服务状态: cd deployments && docker-compose ps"
echo "查看日志: cd deployments && docker-compose logs -f"
echo ""
