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