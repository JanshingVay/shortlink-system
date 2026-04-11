-- scripts/init.sql
-- 容器启动时会自动执行这个脚本，省去我们手动建库的麻烦
CREATE DATABASE IF NOT EXISTS `shortlink` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;