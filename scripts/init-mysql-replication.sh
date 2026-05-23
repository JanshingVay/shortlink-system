#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/../deployments/.env"

if [ -f "$ENV_FILE" ]; then
  export $(grep -v '^#' "$ENV_FILE" | xargs)
fi

MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD:-secure_root_password_123!}
REPL_PASSWORD=${REPL_PASSWORD:-replication_secure_password_321!}

echo "等待 MySQL 主库启动..."
sleep 30

echo "在主库上创建复制用户..."
docker exec shortlink-mysql-master mysql -uroot -p${MYSQL_ROOT_PASSWORD} -e "
CREATE USER IF NOT EXISTS 'repl'@'%' IDENTIFIED BY '${REPL_PASSWORD}';
GRANT REPLICATION SLAVE ON *.* TO 'repl'@'%';
FLUSH PRIVILEGES;
"

echo "获取主库的 binlog 位置..."
MASTER_STATUS=$(docker exec shortlink-mysql-master mysql -uroot -p${MYSQL_ROOT_PASSWORD} -e "SHOW MASTER STATUS\G" | grep -E "File|Position")
MASTER_LOG_FILE=$(echo "$MASTER_STATUS" | grep "File:" | awk '{print $2}')
MASTER_LOG_POS=$(echo "$MASTER_STATUS" | grep "Position:" | awk '{print $2}')

echo "主库状态: File=$MASTER_LOG_FILE, Position=$MASTER_LOG_POS"

echo "配置从库1..."
docker exec shortlink-mysql-slave1 mysql -uroot -p${MYSQL_ROOT_PASSWORD} -e "
STOP SLAVE;
CHANGE MASTER TO
  MASTER_HOST='mysql-master',
  MASTER_USER='repl',
  MASTER_PASSWORD='${REPL_PASSWORD}',
  MASTER_LOG_FILE='$MASTER_LOG_FILE',
  MASTER_LOG_POS=$MASTER_LOG_POS;
START SLAVE;
"

echo "配置从库2..."
docker exec shortlink-mysql-slave2 mysql -uroot -p${MYSQL_ROOT_PASSWORD} -e "
STOP SLAVE;
CHANGE MASTER TO
  MASTER_HOST='mysql-master',
  MASTER_USER='repl',
  MASTER_PASSWORD='${REPL_PASSWORD}',
  MASTER_LOG_FILE='$MASTER_LOG_FILE',
  MASTER_LOG_POS=$MASTER_LOG_POS;
START SLAVE;
"

echo "检查从库1复制状态..."
docker exec shortlink-mysql-slave1 mysql -uroot -p${MYSQL_ROOT_PASSWORD} -e "SHOW SLAVE STATUS\G" | grep -E "Slave_IO_Running|Slave_SQL_Running"

echo "检查从库2复制状态..."
docker exec shortlink-mysql-slave2 mysql -uroot -p${MYSQL_ROOT_PASSWORD} -e "SHOW SLAVE STATUS\G" | grep -E "Slave_IO_Running|Slave_SQL_Running"

echo "MySQL 主从复制配置完成！"
