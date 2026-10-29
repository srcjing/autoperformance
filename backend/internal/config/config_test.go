package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDefaults 验证：无配置文件时使用默认值（缺文件降级不报错）。
func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("Load() 缺文件应降级返回默认值，得到错误: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"server.host", cfg.Server.Host, "0.0.0.0"},
		{"server.port", cfg.Server.Port, 8080},
		{"server.mode", cfg.Server.Mode, "release"},
		{"server.static_dir", cfg.Server.StaticDir, "./web/dist"},
		{"server.tmp_dir", cfg.Server.TmpDir, "./data/tmp"},
		{"jwt.expire_seconds", cfg.JWT.ExpireSeconds, 86400},
		{"security.kek_path", cfg.Security.KEKPath, "./data/keys/kek.key"},
		{"security.report_dir", cfg.Security.ReportDir, "./data/reports"},
		{"security.secret_key_env", cfg.Security.SecretKeyEnv, "AUTO_PERF_SECRET_KEY"},
		{"report.max_size_bytes", cfg.Report.MaxSizeBytes, int64(50 * 1024 * 1024)},
		{"report.gen_timeout_seconds", cfg.Report.GenTimeoutSeconds, 120},
		{"report.chromium_path", cfg.Report.ChromiumPath, "chromium"},
		{"k6.binary_path", cfg.K6.BinaryPath, "k6"},
		{"k6.max_vus", cfg.K6.MaxVUs, 10000},
		{"k6.work_dir", cfg.K6.WorkDir, "./data/k6"},
		{"cleaner.low_priv_uid", cfg.Cleaner.LowPrivUID, 65534},
		{"cleaner.low_priv_gid", cfg.Cleaner.LowPrivGID, 65534},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestLoadFileOverridesDefaults 验证：配置文件覆盖默认值。
func TestLoadFileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  port: 9090
  mode: debug
k6:
  max_vus: 500
report:
  max_size_bytes: 1024
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("server.port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.Mode != "debug" {
		t.Errorf("server.mode = %q, want debug", cfg.Server.Mode)
	}
	if cfg.K6.MaxVUs != 500 {
		t.Errorf("k6.max_vus = %d, want 500", cfg.K6.MaxVUs)
	}
	if cfg.Report.MaxSizeBytes != 1024 {
		t.Errorf("report.max_size_bytes = %d, want 1024", cfg.Report.MaxSizeBytes)
	}
	// 未覆盖的字段仍为默认值
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("server.host = %q, want 0.0.0.0", cfg.Server.Host)
	}
}

// TestLoadEnvOverridesFile 验证：环境变量最终覆盖（AUTO_PERF_ 前缀）。
func TestLoadEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AUTO_PERF_SERVER_PORT", "7070")
	t.Setenv("AUTO_PERF_K6_MAX_VUS", "1234")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	if cfg.Server.Port != 7070 {
		t.Errorf("server.port = %d, want 7070 (env override)", cfg.Server.Port)
	}
	if cfg.K6.MaxVUs != 1234 {
		t.Errorf("k6.max_vus = %d, want 1234 (env override)", cfg.K6.MaxVUs)
	}
}
