// Package config 负责加载应用配置（YAML / 环境变量）。
//
// 采用 viper：默认值兜底 + 配置文件覆盖 + 环境变量（AUTO_PERF_ 前缀）最终覆盖。
// 配置项对齐 architecture.md：server / database / jwt / security / report / k6 / cleaner。
package config

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 顶层配置。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Security SecurityConfig `mapstructure:"security"`
	Report   ReportConfig   `mapstructure:"report"`
	K6       K6Config       `mapstructure:"k6"`
	Cleaner  CleanerConfig  `mapstructure:"cleaner"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	Mode      string `mapstructure:"mode"` // debug | release | test
	StaticDir string `mapstructure:"static_dir"`
	TmpDir    string `mapstructure:"tmp_dir"`
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

// SecurityConfig 安全/密钥配置（对齐 security-design-report.md DES-I-002）。
type SecurityConfig struct {
	// KEKPath 根密钥文件路径（首次安装生成，权限 0600，服务账号属主）。
	KEKPath string `mapstructure:"kek_path"`
	// ReportDir 报告产物落盘目录。
	ReportDir string `mapstructure:"report_dir"`
	// SecretKeyEnv 从环境变量读取额外秘密的变量名（避免硬编码）。
	SecretKeyEnv string `mapstructure:"secret_key_env"`
	// AuditHMACKey 审计链 HMAC 密钥（INV-8 / DES-T-006）。为空时依次回退到
	// SecretKeyEnv 指向的环境变量、JWT secret 派生；生产应显式配置并支持轮换。
	AuditHMACKey string `mapstructure:"audit_hmac_key"`
}

// ReportConfig 报告生成配置。
type ReportConfig struct {
	// MaxSizeBytes 导入报告文件大小上限（字节）。
	MaxSizeBytes int64 `mapstructure:"max_size_bytes"`
	// GenTimeoutSeconds 报告生成（Chromium/gooxml）超时秒数。
	GenTimeoutSeconds int `mapstructure:"gen_timeout_seconds"`
	// ChromiumPath headless Chromium 二进制路径。
	ChromiumPath string `mapstructure:"chromium_path"`
}

// K6Config k6 压测引擎配置。
type K6Config struct {
	// BinaryPath k6 二进制路径。
	BinaryPath string `mapstructure:"binary_path"`
	// MaxVUs 并发数上限（安全白名单，对齐 DES-T-001）。
	MaxVUs int `mapstructure:"max_vus"`
	// WorkDir k6 工作目录（脚本/结果落盘沙箱）。
	WorkDir string `mapstructure:"work_dir"`
}

// CleanerConfig 环境清理器配置。
type CleanerConfig struct {
	// LowPrivUID/GID 清理器/k6 子进程运行的低权限账号（非 root）。
	LowPrivUID int `mapstructure:"low_priv_uid"`
	LowPrivGID int `mapstructure:"low_priv_gid"`
	// AllowedHostIDs 可信清理主机白名单（逗号分隔的 ssh_host ID，DES-T-003）。
	// 为空时拒绝一切远程清理（安全默认），由系统设置 SSH/Host 录入后配置。
	AllowedHostIDs string `mapstructure:"allowed_host_ids"`
	// AllowedScriptHashes 清理脚本内容 SHA256 白名单（逗号分隔，DES-T-003）。
	AllowedScriptHashes string `mapstructure:"allowed_script_hashes"`
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

	// 配置文件不存在时使用默认值（允许无配置文件直接启动，缺文件降级）。
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !os.IsNotExist(err) {
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
	v.SetDefault("server.static_dir", "./web/dist")
	v.SetDefault("server.tmp_dir", "./data/tmp")

	v.SetDefault("database.host", "127.0.0.1")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "postgres")
	v.SetDefault("database.password", "postgres")
	v.SetDefault("database.name", "auto_performance")
	v.SetDefault("database.sslmode", "disable")

	v.SetDefault("jwt.secret", "auto_performance-dev-secret-change-me")
	v.SetDefault("jwt.expire_seconds", 86400)

	v.SetDefault("security.kek_path", "./data/keys/kek.key")
	v.SetDefault("security.report_dir", "./data/reports")
	v.SetDefault("security.secret_key_env", "AUTO_PERF_SECRET_KEY")
	v.SetDefault("security.audit_hmac_key", "")

	v.SetDefault("report.max_size_bytes", 50*1024*1024) // 50 MB
	v.SetDefault("report.gen_timeout_seconds", 120)
	v.SetDefault("report.chromium_path", "chromium")

	v.SetDefault("k6.binary_path", "k6")
	v.SetDefault("k6.max_vus", 10000)
	v.SetDefault("k6.work_dir", "./data/k6")

	v.SetDefault("cleaner.low_priv_uid", 65534) // nobody
	v.SetDefault("cleaner.low_priv_gid", 65534)
	v.SetDefault("cleaner.allowed_host_ids", "")
	v.SetDefault("cleaner.allowed_script_hashes", "")
}

// bindEnv 显式绑定环境变量（AUTO_PERF_ 前缀），保证 Unmarshal 能读到环境变量覆盖。
func bindEnv(v *viper.Viper) {
	_ = v.BindEnv("server.host", "AUTO_PERF_SERVER_HOST")
	_ = v.BindEnv("server.port", "AUTO_PERF_SERVER_PORT")
	_ = v.BindEnv("server.mode", "AUTO_PERF_SERVER_MODE")
	_ = v.BindEnv("server.static_dir", "AUTO_PERF_SERVER_STATIC_DIR")
	_ = v.BindEnv("server.tmp_dir", "AUTO_PERF_SERVER_TMP_DIR")

	_ = v.BindEnv("database.host", "AUTO_PERF_DATABASE_HOST")
	_ = v.BindEnv("database.port", "AUTO_PERF_DATABASE_PORT")
	_ = v.BindEnv("database.user", "AUTO_PERF_DATABASE_USER")
	_ = v.BindEnv("database.password", "AUTO_PERF_DATABASE_PASSWORD")
	_ = v.BindEnv("database.name", "AUTO_PERF_DATABASE_NAME")
	_ = v.BindEnv("database.sslmode", "AUTO_PERF_DATABASE_SSLMODE")

	_ = v.BindEnv("jwt.secret", "AUTO_PERF_JWT_SECRET")
	_ = v.BindEnv("jwt.expire_seconds", "AUTO_PERF_JWT_EXPIRE_SECONDS")

	_ = v.BindEnv("security.kek_path", "AUTO_PERF_SECURITY_KEK_PATH")
	_ = v.BindEnv("security.report_dir", "AUTO_PERF_SECURITY_REPORT_DIR")
	_ = v.BindEnv("security.secret_key_env", "AUTO_PERF_SECURITY_SECRET_KEY_ENV")
	_ = v.BindEnv("security.audit_hmac_key", "AUTO_PERF_SECURITY_AUDIT_HMAC_KEY")

	_ = v.BindEnv("report.max_size_bytes", "AUTO_PERF_REPORT_MAX_SIZE_BYTES")
	_ = v.BindEnv("report.gen_timeout_seconds", "AUTO_PERF_REPORT_GEN_TIMEOUT_SECONDS")
	_ = v.BindEnv("report.chromium_path", "AUTO_PERF_REPORT_CHROMIUM_PATH")

	_ = v.BindEnv("k6.binary_path", "AUTO_PERF_K6_BINARY_PATH")
	_ = v.BindEnv("k6.max_vus", "AUTO_PERF_K6_MAX_VUS")
	_ = v.BindEnv("k6.work_dir", "AUTO_PERF_K6_WORK_DIR")

	_ = v.BindEnv("cleaner.low_priv_uid", "AUTO_PERF_CLEANER_LOW_PRIV_UID")
	_ = v.BindEnv("cleaner.low_priv_gid", "AUTO_PERF_CLEANER_LOW_PRIV_GID")
	_ = v.BindEnv("cleaner.allowed_host_ids", "AUTO_PERF_CLEANER_ALLOWED_HOST_IDS")
	_ = v.BindEnv("cleaner.allowed_script_hashes", "AUTO_PERF_CLEANER_ALLOWED_SCRIPT_HASHES")

	// 环境变量命名约定：AUTO_PERF_SERVER_PORT 等；用于文档说明。
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetEnvPrefix("AUTO_PERF")
	v.AutomaticEnv()
}
