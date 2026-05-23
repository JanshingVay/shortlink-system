# repository 包 - 存储层模块说明
模块概述
repository 包是短链接系统的核心存储层，封装了 MySQL（基于 GORM 框架）和 Redis（基于 go-redis v9 库）的客户端初始化、连接池调优逻辑，为上层业务提供稳定、高效的存储访问能力，专门针对高并发场景做了连接池参数优化，保证系统在高流量下的稳定性。
核心结构体说明
1. Storage
封装了 MySQL 和 Redis 的客户端实例，上层业务层通过该结构体操作数据库 / 缓存，实现存储能力的统一管理。
type Storage struct {
    DB    *gorm.DB       // GORM 封装的 MySQL 客户端
    Redis *redis.Client  // go-redis 封装的 Redis 客户端
}
2. Config
存储层初始化配置参数，支持从配置文件（如 YAML）注入，适配不同环境（开发 / 测试 / 生产）的配置差异。
type Config struct {
    MySQLDSN  string // MySQL 数据源地址，格式：user:pass@tcp(127.0.0.1:3306)/dbname?charset=utf8mb4&parseTime=True&loc=Local
    RedisAddr string // Redis 地址，格式：127.0.0.1:6379
    RedisPass string // Redis 密码（无密码则为空字符串）
}
核心方法：NewStorage
功能
初始化并返回配置好高并发连接池的 Storage 实例，包含 MySQL 和 Redis 的完整初始化流程，以及连接可用性检测。
初始化流程拆解
1. MySQL 初始化（GORM）
日志优化：生产环境下将 GORM 日志级别设为 Silent，避免日志 I/O 拖慢高并发场景性能；
连接池调优（核心）：
参数	配置值	作用说明
SetMaxOpenConns	100	限制最大活跃连接数，防止高并发下创建无限连接撑爆 MySQL 服务
SetMaxIdleConns	20	保留空闲连接，避免频繁创建 / 销毁连接的性能开销
SetConnMaxLifetime	1 小时	强制替换旧连接，防止复用被 MySQL 主动断开的 “死连接”（MySQL 默认 8 小时断开空闲连接）
表结构迁移：自动迁移 model.ShortLink 表结构（仅开发环境使用，生产环境需用 SQL 脚本手动迁移）。
2. Redis 初始化（go-redis v9）
连接池调优：
参数	配置值	作用说明
PoolSize	100	连接池最大连接数，适配高并发场景连接需求
MinIdleConns	10	保持最小空闲连接数，减少连接创建开销
连接检测：通过 5 秒超时的 PING 命令检测 Redis 连接可用性，确保初始化成功后 Redis 可正常使用。
使用示例
package main

import (
    "log"
    "shortlink-system/internal/repository"
)

func main() {
    // 1. 构建配置（实际项目中建议从 YAML/环境变量读取）
    cfg := repository.Config{
        MySQLDSN:  "root:123456@tcp(127.0.0.1:3306)/shortlink?charset=utf8mb4&parseTime=True&loc=Local",
        RedisAddr: "127.0.0.1:6379",
        RedisPass: "", // 无密码
    }

    // 2. 初始化存储层
    storage, err := repository.NewStorage(cfg)
    if err != nil {
        log.Fatalf("存储层初始化失败: %v", err)
    }

    // 3. 业务层使用（示例：查询短链接）
    // shortLink := &model.ShortLink{}
    // if err := storage.DB.Where("short_code = ?", "abc123").First(shortLink).Error; err != nil {
    //     log.Printf("查询失败: %v", err)
    // }

    // 4. 缓存使用（示例：设置短链接缓存）
    // ctx := context.Background()
    // if err := storage.Redis.Set(ctx, "short:abc123", "https://example.com", 24*time.Hour).Err(); err != nil {
    //     log.Printf("缓存设置失败: %v", err)
    // }
}
注意事项
环境适配：
开发环境：可开启 GORM 日志（将 logger.Silent 改为 logger.Info），便于调试；
生产环境：必须关闭 AutoMigrate 自动迁移，改用 SQL 脚本手动执行表结构变更，避免数据风险。
连接池参数调整：
可根据服务器配置、业务并发量调整 SetMaxOpenConns/PoolSize 等参数（例如单机 MySQL 建议最大连接数不超过 200）；
Redis PoolSize 建议不超过 Redis 服务端配置的 maxclients（默认 10000）。
错误处理：初始化失败时会返回包装后的错误（如 连接 MySQL 失败/连接 Redis 失败），上层需捕获并处理，避免服务静默启动失败。
资源释放：
MySQL 连接池由 GORM 自动管理，程序退出时可通过 sqlDB.Close() 手动关闭；
Redis 客户端可通过 rdb.Close() 关闭连接池，建议在程序退出钩子中执行。
核心设计亮点
高并发适配：针对 MySQL/Redis 连接池做专项调优，解决高并发下连接耗尽、性能下降问题；
生产级细节：区分开发 / 生产环境的表迁移方式、关闭冗余日志、连接超时检测，符合生产环境规范；
易用性：封装统一的 Storage 结构体，上层业务无需关注底层客户端初始化细节，降低耦合；
可扩展性：Config 结构体预留配置扩展能力，可后续新增超时时间、数据库编号等参数。