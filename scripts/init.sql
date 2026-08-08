-- scripts/init.sql
-- 容器启动时会自动执行这个脚本，省去我们手动建库的麻烦
CREATE DATABASE IF NOT EXISTS `shortlink` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

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

CREATE TABLE IF NOT EXISTS `outbox_messages` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键',
  `short_code` VARCHAR(10) NOT NULL COMMENT '短码',
  `long_url` VARCHAR(2048) NOT NULL COMMENT '原始长链接',
  `url_hash` CHAR(32) NOT NULL COMMENT 'long_url的MD5哈希',
  `status` VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT '消息状态 pending/processing/sent/failed',
  `retry_count` INT NOT NULL DEFAULT 0 COMMENT '重试次数',
  `worker_id` VARCHAR(128) NOT NULL DEFAULT '' COMMENT '领取消息的Worker',
  `locked_until` DATETIME(3) NULL DEFAULT NULL COMMENT '租约过期时间',
  `created_at` DATETIME(3) NULL DEFAULT NULL COMMENT '创建时间',
  `updated_at` DATETIME(3) NULL DEFAULT NULL COMMENT '更新时间',
  PRIMARY KEY (`id`),
  KEY `idx_outbox_status_created_at` (`status`, `created_at`),
  KEY `idx_outbox_status_locked_until_created_at` (`status`, `locked_until`, `created_at`),
  KEY `idx_outbox_status_updated_at` (`status`, `updated_at`),
  KEY `idx_outbox_retry_count` (`retry_count`)
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_unicode_ci
  COMMENT='事务发件箱表';
