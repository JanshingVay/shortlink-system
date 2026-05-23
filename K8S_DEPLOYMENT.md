# 短链接系统 Kubernetes 部署指南

## 架构概述

| 组件 | K8s 资源 | 副本数 |
|------|---------|--------|
| MySQL 主库 | StatefulSet | 1 |
| MySQL 从库 | StatefulSet | 2 |
| Redis 集群 | StatefulSet | 6 |
| 应用服务 | Deployment | 3 (可自动扩展) |
| Ingress | Ingress | 1 |

## 前置要求

- Kubernetes 1.20+
- kubectl 配置完成
- nginx-ingress-controller 已安装
- 持久化存储可用

## 快速开始

### 1. 配置密码

```bash
# 编辑 secret.yaml，修改 base64 编码后的密码
echo -n "your_strong_password" | base64
```

### 2. 部署资源

```bash
# 创建命名空间
kubectl apply -f k8s/namespace.yaml

# 部署配置
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.yaml

# 部署 MySQL
kubectl apply -f k8s/mysql/

# 部署 Redis
kubectl apply -f k8s/redis/

# 部署应用
kubectl apply -f k8s/app/

# 部署 Ingress
kubectl apply -f k8s/ingress/
```

### 3. 初始化集群

```bash
# 等待 Pod 启动
kubectl get pods -n shortlink-system -w

# 初始化 MySQL 主从（需要手动执行 SQL）
# 初始化 Redis 集群
```

### 4. 配置 hosts

```bash
# 添加到 /etc/hosts
<ingress-ip> shortlink.local
```

## 资源规格说明

### MySQL
- CPU: 500m ~ 2000m
- 内存: 512Mi ~ 2Gi
- 存储: 10Gi

### Redis
- CPU: 200m ~ 1000m
- 内存: 256Mi ~ 1Gi
- 存储: 5Gi

### 应用
- CPU: 200m ~ 1000m
- 内存: 256Mi ~ 1Gi
- HPA: 3 ~ 10 副本

## 常用命令

```bash
# 查看 Pod
kubectl get pods -n shortlink-system

# 查看日志
kubectl logs -f deployment/shortlink-app -n shortlink-system

# 扩容
kubectl scale deployment shortlink-app --replicas=5 -n shortlink-system

# 查看 HPA
kubectl get hpa -n shortlink-system

# 删除所有资源
kubectl delete namespace shortlink-system
```

## 监控和维护

- 使用 Prometheus + Grafana 监控
- 配置日志收集（ELK 或 Loki）
- 定期备份数据库
