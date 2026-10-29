// Package logutil 提供日志写入前的统一脱敏过滤器。
//
// 对齐 security-design-report.md DES-I-001 / CODE-I-001（日志模块）：
// 剥离 Authorization 头、password= 口令、私钥 PEM 特征，禁止明文落日志。
package logutil

import "regexp"

var (
	// 剥离 Authorization 头完整凭据（scheme + token），如 "Bearer <jwt>"、"Basic <base64>"。
	// 仅匹配已知认证 scheme，保证幂等（替换产物 "***" 不会再次命中）。
	authHeaderRE = regexp.MustCompile(`(?i)(authorization\s*:\s*)(?:bearer|basic|digest)\s+\S+`)
	passwordRE   = regexp.MustCompile(`(?i)(password\s*=\s*)\S+`)
	privateKeyRE = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)

	// JSON 形态的认证头/口令字段（WS 推送/结构化日志常以 {"Authorization":"..."} 形式出现）。
	// 保留键名与冒号，仅抹掉值，且保证幂等。
	jsonAuthHeaderRE = regexp.MustCompile(`(?i)("(?:authorization|proxy-authorization|cookie|set-cookie|x-api-key|x-auth-token|x-access-token|token)"\s*:\s*")(?:bearer\s+)?[^"]*"`)
	jsonPasswordRE   = regexp.MustCompile(`(?i)("(?:password|passwd|secret)"\s*:\s*")[^"]*"`)
)

// SanitizeLog 对字符串做脱敏：剥离认证头/口令/私钥特征。
// 保留键名前缀（如 "Authorization: "），仅替换值部分为 ***。
func SanitizeLog(s string) string {
	s = authHeaderRE.ReplaceAllString(s, "${1}***")
	s = passwordRE.ReplaceAllString(s, "${1}***")
	s = jsonAuthHeaderRE.ReplaceAllString(s, "${1}***\"")
	s = jsonPasswordRE.ReplaceAllString(s, "${1}***\"")
	s = privateKeyRE.ReplaceAllString(s, "***")
	return s
}
