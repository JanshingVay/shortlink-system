package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	App      AppConfig      `mapstructure:"app"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

type DatabaseConfig struct {
	MySQL struct {
		MasterDSN string   `mapstructure:"master_dsn"`
		SlaveDSNs []string `mapstructure:"slave_dsns"`
	} `mapstructure:"mysql"`
	Redis struct {
		ClusterAddrs []string `mapstructure:"cluster_addrs"`
		Pass         string   `mapstructure:"pass"`
	} `mapstructure:"redis"`
}

type AppConfig struct {
	NodeID    int64  `mapstructure:"node_id"`
	URLPrefix string `mapstructure:"url_prefix"`
}

func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	bindEnv(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	applyEnvSlices(v, &cfg)

	return &cfg, nil
}

func bindEnv(v *viper.Viper) {
	bindings := map[string][]string{
		"server.mode":                  {"SERVER_MODE", "GIN_MODE"},
		"server.port":                  {"SERVER_PORT"},
		"database.mysql.master_dsn":    {"DATABASE_MYSQL_MASTER_DSN", "MYSQL_MASTER_DSN"},
		"database.mysql.slave_dsns":    {"DATABASE_MYSQL_SLAVE_DSNS", "MYSQL_SLAVE_DSNS"},
		"database.redis.cluster_addrs": {"DATABASE_REDIS_CLUSTER_ADDRS", "REDIS_CLUSTER_ADDRS"},
		"database.redis.pass":          {"DATABASE_REDIS_PASS", "REDIS_PASSWORD"},
		"app.node_id":                  {"APP_NODE_ID"},
		"app.url_prefix":               {"APP_URL_PREFIX", "URL_PREFIX"},
	}

	for key, envs := range bindings {
		_ = v.BindEnv(append([]string{key}, envs...)...)
	}

	_ = v.BindEnv("mysql.slave1_dsn", "MYSQL_SLAVE1_DSN")
	_ = v.BindEnv("mysql.slave2_dsn", "MYSQL_SLAVE2_DSN")
}

func applyEnvSlices(v *viper.Viper, cfg *Config) {
	if slaveDSNs := splitNonEmpty(v.GetString("database.mysql.slave_dsns")); len(slaveDSNs) > 0 {
		cfg.Database.MySQL.SlaveDSNs = slaveDSNs
	}

	var slaveDSNs []string
	if slave1DSN := strings.TrimSpace(v.GetString("mysql.slave1_dsn")); slave1DSN != "" {
		slaveDSNs = append(slaveDSNs, slave1DSN)
	}
	if slave2DSN := strings.TrimSpace(v.GetString("mysql.slave2_dsn")); slave2DSN != "" {
		slaveDSNs = append(slaveDSNs, slave2DSN)
	}
	if len(slaveDSNs) > 0 {
		cfg.Database.MySQL.SlaveDSNs = slaveDSNs
	}

	if redisAddrs := splitNonEmpty(v.GetString("database.redis.cluster_addrs")); len(redisAddrs) > 0 {
		cfg.Database.Redis.ClusterAddrs = redisAddrs
	}
}

func splitNonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
