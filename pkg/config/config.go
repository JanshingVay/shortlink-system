package config

import (
	"fmt"
	"os"
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
	viper.SetConfigFile(path)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	if masterDSN := os.Getenv("MYSQL_MASTER_DSN"); masterDSN != "" {
		cfg.Database.MySQL.MasterDSN = masterDSN
	}

	var slaveDSNs []string
	if slave1DSN := os.Getenv("MYSQL_SLAVE1_DSN"); slave1DSN != "" {
		slaveDSNs = append(slaveDSNs, slave1DSN)
	}
	if slave2DSN := os.Getenv("MYSQL_SLAVE2_DSN"); slave2DSN != "" {
		slaveDSNs = append(slaveDSNs, slave2DSN)
	}
	if len(slaveDSNs) > 0 {
		cfg.Database.MySQL.SlaveDSNs = slaveDSNs
	}

	if redisAddrs := os.Getenv("REDIS_CLUSTER_ADDRS"); redisAddrs != "" {
		cfg.Database.Redis.ClusterAddrs = strings.Split(redisAddrs, ",")
	}

	if redisPass := os.Getenv("REDIS_PASSWORD"); redisPass != "" {
		cfg.Database.Redis.Pass = redisPass
	}

	if ginMode := os.Getenv("GIN_MODE"); ginMode != "" {
		cfg.Server.Mode = ginMode
	}

	if urlPrefix := os.Getenv("URL_PREFIX"); urlPrefix != "" {
		cfg.App.URLPrefix = urlPrefix
	}

	return &cfg, nil
}
