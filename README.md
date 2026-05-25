# shortlink-system

## 本地 MySQL 初始化

```sql
CREATE DATABASE IF NOT EXISTS `shortlink`
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;

USE `shortlink`;

CREATE TABLE IF NOT EXISTS `short_links` (
  `id` BIGINT UNSIGNED NOT NULL COMMENT '雪花算法生成的唯一ID',
  `short_code` VARCHAR(10) NOT NULL COMMENT 'Base62短码',
  `long_url` VARCHAR(2048) NOT NULL COMMENT '原始长链接',
  `long_url_hash` CHAR(32) NOT NULL COMMENT 'long_url的MD5哈希，用于去重',
  `created_at` DATETIME(3) NULL DEFAULT NULL COMMENT '创建时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_short_links_short_code` (`short_code`),
  UNIQUE KEY `idx_short_links_long_url_hash` (`long_url_hash`)
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_unicode_ci
  COMMENT='短链接表';
```

已有旧表（缺少 `long_url_hash` 列）执行以下迁移：

```sql
ALTER TABLE `short_links`
  ADD COLUMN `long_url_hash` CHAR(32) NOT NULL DEFAULT '' COMMENT 'long_url的MD5哈希，用于去重'
  AFTER `long_url`;

UPDATE `short_links` SET `long_url_hash` = MD5(`long_url`);

ALTER TABLE `short_links`
  ADD UNIQUE KEY `idx_short_links_long_url_hash` (`long_url_hash`);
```

## 本地启动

```bash
go run cmd/server/main.go
```

## API 示例

### 长链接转短链接 encode

```bash
curl -X POST 'http://localhost:8080/api/v1/encode' \
  -H 'Content-Type: application/json' \
  -d '{"long_url":"https://github.com/weijianxing/shortlink-system"}'
```

响应示例：

```json
{
  "code": 200,
  "short_url": "http://localhost:8080/7asTNwd4gU"
}
```

### 查询短码对应的长链接 decode

```bash
curl 'http://localhost:8080/api/v1/decode/7asTNwd4gU'
```

响应示例：

```json
{
  "code": 200,
  "short_code": "7asTNwd4gU",
  "long_url": "https://github.com/weijianxing/shortlink-system"
}
```

### 访问短链接跳转

```bash
curl -i 'http://localhost:8080/7asTNwd4gU'
```

成功时会返回 `302 Found`，并通过 `Location` 响应头跳转到原始长链接。

## 压测

### 启动服务 (关闭限流)

压测时建议关闭限流以测试真实业务上限：

```bash
go run cmd/server/main.go --rate-limit=false
```

### 准备种子数据

启动服务后，预先生成一批短码供混合压测读取：

```bash
bash scripts/seed_benchmark.sh http://localhost:8080 1000 seed_codes.txt
```

### 混合读写压测 (推荐)

真实业务场景：90% 读 + 10% 写，写请求中 80% 新链接、读请求中 80% 已存在短码：

```bash
wrk -t4 -c100 -d30s -s benchmark_mixed.lua http://localhost:8080
```

压测结束后会输出分类统计：

```text
------------------------------
混合读写压测报告
------------------------------
总请求数:       123456
读请求数:       111110 (90.0%)
  已存在命中:   88888 (2xx)
  不存在返回:   22222 (404)
  错误:         0
写请求数:       12346 (10.0%)
  成功:         9876 (2xx)
  限流:         0 (429)
  错误:         2470
------------------------------
```