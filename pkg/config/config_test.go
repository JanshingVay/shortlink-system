package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadConfigEnvOverrideForDocker(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`server:
  port: 8080
  mode: "debug"
database:
  mysql:
    master_dsn: "root:123456@tcp(127.0.0.1:3306)/shortlink?charset=utf8mb4&parseTime=True&loc=Local"
    slave_dsns:
      - "root:123456@tcp(127.0.0.1:3307)/shortlink?charset=utf8mb4&parseTime=True&loc=Local"
  redis:
    cluster_addrs:
      - "127.0.0.1:7001"
    pass: ""
app:
  node_id: 1
  url_prefix: "http://localhost:8080/"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	mysqlMasterDSN := "shortlink:pwd@tcp(mysql-master:3306)/shortlink?charset=utf8mb4&parseTime=True&loc=Local"
	mysqlSlave1DSN := "shortlink:pwd@tcp(mysql-slave1:3306)/shortlink?charset=utf8mb4&parseTime=True&loc=Local"
	mysqlSlave2DSN := "shortlink:pwd@tcp(mysql-slave2:3306)/shortlink?charset=utf8mb4&parseTime=True&loc=Local"
	redisAddrs := []string{"redis-node-1:7001", "redis-node-2:7002", "redis-node-3:7003"}

	t.Setenv("MYSQL_MASTER_DSN", mysqlMasterDSN)
	t.Setenv("MYSQL_SLAVE1_DSN", mysqlSlave1DSN)
	t.Setenv("MYSQL_SLAVE2_DSN", mysqlSlave2DSN)
	t.Setenv("REDIS_CLUSTER_ADDRS", "redis-node-1:7001,redis-node-2:7002,redis-node-3:7003")
	t.Setenv("REDIS_PASSWORD", "redis_password")
	t.Setenv("GIN_MODE", "release")
	t.Setenv("URL_PREFIX", "http://localhost/")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	if cfg.Database.MySQL.MasterDSN != mysqlMasterDSN {
		t.Fatalf("MySQL 主库 DSN 未被环境变量覆盖: %s", cfg.Database.MySQL.MasterDSN)
	}
	if !reflect.DeepEqual(cfg.Database.MySQL.SlaveDSNs, []string{mysqlSlave1DSN, mysqlSlave2DSN}) {
		t.Fatalf("MySQL 从库 DSN 未被环境变量覆盖: %#v", cfg.Database.MySQL.SlaveDSNs)
	}
	if !reflect.DeepEqual(cfg.Database.Redis.ClusterAddrs, redisAddrs) {
		t.Fatalf("Redis 集群地址未被环境变量覆盖: %#v", cfg.Database.Redis.ClusterAddrs)
	}
	if cfg.Database.Redis.Pass != "redis_password" {
		t.Fatalf("Redis 密码未被环境变量覆盖: %s", cfg.Database.Redis.Pass)
	}
	if cfg.Server.Mode != "release" {
		t.Fatalf("服务模式未被环境变量覆盖: %s", cfg.Server.Mode)
	}
	if cfg.App.URLPrefix != "http://localhost/" {
		t.Fatalf("URL 前缀未被环境变量覆盖: %s", cfg.App.URLPrefix)
	}
}

func BenchmarkLoadConfig(b *testing.B) {
	configPath := filepath.Join(b.TempDir(), "config.yaml")
	content := []byte(`server:
  port: 8080
  mode: "release"
database:
  mysql:
    master_dsn: "root:password@tcp(127.0.0.1:3306)/shortlink"
    slave_dsns: []
  redis:
    cluster_addrs:
      - "127.0.0.1:6379"
    pass: ""
app:
  node_id: 1
  url_prefix: "http://localhost/"
mq:
  rabbitmq:
    url: "amqp://guest:guest@localhost:5672/"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		b.Fatalf("写入基准配置失败: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadConfig(configPath); err != nil {
			b.Fatal(err)
		}
	}
}
