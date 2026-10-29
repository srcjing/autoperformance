// Package setting 系统设置业务服务（管理员/角色/证书/SSH-Host）。
package setting

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"

	"auto_performance/internal/crypto"
)

// LoadKEK 加载信封加密根密钥（KEK），供证书私钥 / SSH 私钥加密使用。
//
// 密钥来源优先级（DES-I-008：密钥不硬编码、可轮换）：
//  1. security.kek_path 指向的 KEK 文件（安装脚本生成，权限 0600）；文件内容支持
//     32 字节原始密钥、64 位 hex 或 base64 三种写法；
//  2. security.secret_key_env 指向的环境变量（运维下发，避免落盘）；
//  3. JWT secret 派生（仅开发/降级兜底，生产必须配置 1 或 2）。
//
// KEK 文件存在但内容非法时**失败关闭**（返回 error），不会静默降级到派生密钥，
// 否则历史密文将永久无法解密。
//
// source 为密钥来源描述（仅用于启动日志提示，不含密钥内容）。
func LoadKEK(kekPath, envSecret, fallbackSecret string) (key []byte, source string, err error) {
	if path := strings.TrimSpace(kekPath); path != "" {
		loaded, loadErr := loadKEKFile(path)
		if loadErr == nil {
			return loaded, "file:" + path, nil
		}
		if !os.IsNotExist(loadErr) {
			return nil, "", loadErr
		}
		// 文件不存在：降级到环境变量/派生（开发环境与未初始化 KEK 的部署）。
	}

	if material := strings.TrimSpace(envSecret); material != "" {
		sum := sha256.Sum256([]byte("auto_performance/kek/v1|env|" + material))
		return sum[:], "env", nil
	}
	if material := strings.TrimSpace(fallbackSecret); material != "" {
		sum := sha256.Sum256([]byte("auto_performance/kek/v1|derived|" + material))
		return sum[:], "derived-from-jwt-secret", nil
	}
	return nil, "", fmt.Errorf("KEK 不可用：kek_path 缺失且未提供 secret_key_env/JWT secret")
}

// loadKEKFile 读取并校验 KEK 文件（长度必须是 32 字节，权限不得放宽到组/其他用户）。
func loadKEKFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	// 权限收敛检查（DES-I-008）：仅非 Windows 平台校验 POSIX 权限位。
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("KEK 文件权限过宽（%#o），要求 0600: %s", info.Mode().Perm(), path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 KEK 文件失败: %w", err)
	}
	return parseKEK(raw)
}

// parseKEK 兼容原始 32 字节 / hex / base64 三种 KEK 文件写法。
func parseKEK(raw []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("KEK 文件内容为空")
	}

	if len(trimmed) == crypto.KeySize {
		return []byte(trimmed), nil
	}
	if len(trimmed) == crypto.KeySize*2 {
		if key, err := hex.DecodeString(trimmed); err == nil && len(key) == crypto.KeySize {
			return key, nil
		}
	}
	if key, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(key) == crypto.KeySize {
		return key, nil
	}
	if len(raw) == crypto.KeySize {
		return raw, nil
	}
	return nil, fmt.Errorf("KEK 文件内容非法：期望 %d 字节原始密钥/hex/base64", crypto.KeySize)
}
