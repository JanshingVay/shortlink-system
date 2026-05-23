# 短链接系统分布式部署指南

## 架构概述

本系统采用以下分布式架构：
- **MySQL**: 1主2从，读写分离
- **Redis**: 6节点集群（3主3从）
- **应用服务**: 3个实例
- **Nginx**: 负载均衡，对外暴露服务

## 前置要求

- Docker 20.10+
- Docker Compose 2.0+
- 至少4核CPU，8GB内存

## 快速开始

### 1. 配置环境变量

编辑 `deployments/.env` 文件，修改默认密码：

```bash
cd deployments
vim .env
```

修改为强密码：
```
MYSQL_ROOT_PASSWORD=your_strong_root_password
MYSQL_PASSWORD=your_strong_shortlink_password
REDIS_PASSWORD=your_strong_redis_password
```

### 2. 启动所有服务

```bash
cd deployments
docker-compose up -d
```

### 3. 初始化 MySQL 主从复制

```bash
cd ../scripts
chmod +x init-mysql-replication.sh
./init-mysql-replication.sh
```

### 4. 初始化 Redis 集群

```bash
chmod +x init-redis-cluster.sh
./init-redis-cluster.sh
```

### 5. 验证部署

检查所有容器状态：
```bash
cd ../deployments
docker-compose ps
```

访问服务：
```bash
curl http://localhost/health
```

## 详细说明

### 服务端口映射

- **Nginx**: 80 (对外暴露)
- **MySQL Master**: 3306 (仅内部网络)
- **MySQL Slave1/2**: 3307, 3308 (仅内部网络)
- **Redis Nodes**: 7001-7006 (仅内部网络)
- **App Services**: 8080 (仅内部网络)

### 数据持久化

所有数据通过 Docker Volumes 持久化：
- MySQL 主从数据: `mysql_master_data`, `mysql_slave1_data`, `mysql_slave2_data`
- Redis 节点数据: `redis_node_1_data` 到 `redis_node_6_data`

### 安全配置

1. **网络隔离**: 所有服务在 `shortlink-network` 私有网络
2. **密码保护**: MySQL 和 Redis 都设置密码
3. **只读模式**: MySQL 从库设置为只读
4. **生产模式**: 应用运行在 `release` 模式

## 监控和维护

### 查看日志

```bash
# 查看所有服务日志
docker-compose logs -f

# 查看特定服务日志
docker-compose logs -f nginx
docker-compose logs -f shortlink-app-1
```

### 扩容应用服务

修改 `docker-compose.yml`，添加更多 `shortlink-app-N` 服务，并更新 `nginx.conf` 中的 upstream 配置。

### 备份数据

```bash
# 备份 MySQL
docker exec shortlink-mysql-master mysqldump -uroot -p shortlink > backup.sql

# 备份 Redis
docker exec shortlink-redis-node-1 redis-cli -a your_redis_password BGSAVE
```

## 常见问题

### 1. 容器启动失败

检查日志：
```bash
docker-compose logs [service-name]
```

### 2. Redis 集群创建失败

确保所有 6 个 Redis 节点都已启动：
```bash
docker-compose ps | grep redis
```

### 3. MySQL 复制失败

检查从库状态：
```bash
docker exec shortlink-mysql-slave1 mysql -uroot -p -e "SHOW SLAVE STATUS\G"
```

## 停止和清理

```bash
# 停止所有服务
cd deployments
docker-compose down

# 停止并删除数据卷（谨慎操作）
docker-compose down -v
```
