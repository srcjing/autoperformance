// Package config 负责加载应用配置（YAML / 环境变量）。
//
// 采用 viper：默认值兜底 + 配置文件覆盖 + 环境变量（AUTO_PERF_ 前缀）最终覆盖。
// 骨架阶段仅包含 server / database / jwt 三类配置项，后续按需扩展。
package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 顶层配置。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	JWT      JWTConfig      `mapstructure:"jwt"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug | release | test
}

// DatabaseConfig PostgreSQL 配置。
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslmode"`
}

// JWTConfig 认证配置。
type JWTConfig struct {
	Secret        string `mapstructure:"secret"`
	ExpireSeconds int    `mapstructure:"expire_seconds"`
}

// TTL 返回 JWT 有效期，缺省 24h。
func (j JWTConfig) TTL() time.Duration {
	if j.ExpireSeconds <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(j.ExpireSeconds) * time.Second
}

// Load 加载配置。cfgPath 非空时按指定文件读取；否则按约定搜索 config.{yaml,yml,json}。
func Load(cfgPath string) (*Config, error) {
	v := viper.New()

	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("./backend/config")
	}

	setDefaults(v)
	bindEnv(v)

	// 配置文件不存在时使用默认值（骨架允许无配置文件直接启动）。
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "release")

	v.SetDefault("database.host", "127.0.0.1")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "postgres")
	v.SetDefault("database.password", "postgres")
	v.SetDefault("database.name", "auto_performance")
	v.SetDefault("database.sslmode", "disable")

	v.SetDefault("jwt.secret", "auto_performance-dev-secret-change-me")
	v.SetDefault("jwt.expire_seconds", 86400)
}

// bindEnv 显式绑定环境变量（AUTO_PERF_ 前缀），保证 Unmarshal 能读到环境变量覆盖。
func bindEnv(v *viper.Viper) {
	_ = v.BindEnv("server.host", "AUTO_PERF_SERVER_HOST")
	_ = v.BindEnv("server.port", "AUTO_PERF_SERVER_PORT")
	_ = v.BindEnv("server.mode", "AUTO_PERF_SERVER_MODE")

	_ = v.BindEnv("database.host", "AUTO_PERF_DATABASE_HOST")
	_ = v.BindEnv("database.port", "AUTO_PERF_DATABASE_PORT")
	_ = v.BindEnv("database.user", "AUTO_PERF_DATABASE_USER")
	_ = v.BindEnv("database.password", "AUTO_PERF_DATABASE_PASSWORD")
	_ = v.BindEnv("database.name", "AUTO_PERF_DATABASE_NAME")
	_ = v.BindEnv("database.sslmode", "AUTO_PERF_DATABASE_SSLMODE")

	_ = v.BindEnv("jwt.secret", "AUTO_PERF_JWT_SECRET")
	_ = v.BindEnv("jwt.expire_seconds", "AUTO_PERF_JWT_EXPIRE_SECONDS")

	// 环境变量命名约定：AUTO_PERF_SERVER_PORT 等；用于文档说明。
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetEnvPrefix("AUTO_PERF")
	v.AutomaticEnv()
}
