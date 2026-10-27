package logutil

import (
	"strings"
	"testing"
)

func TestSanitizeLogStripsSecret(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "authorization bearer jwt",
			in:   "req Authorization: Bearer eyJhbGciOi.token.secret done",
			want: "req Authorization: *** done",
		},
		{
			name: "authorization basic",
			in:   "Authorization: Basic dXNlcjpwYXNz",
			want: "Authorization: ***",
		},
		{
			name: "password assignment",
			in:   "login password=Sup3rSecret!123 ok",
			want: "login password=*** ok",
		},
		{
			name: "rsa private key",
			in:   "key -----BEGIN RSA PRIVATE KEY----- body",
			want: "key *** body",
		},
		{
			name: "ec private key",
			in:   "-----BEGIN EC PRIVATE KEY-----",
			want: "***",
		},
		{
			name: "no secret untouched",
			in:   "task started normally",
			want: "task started normally",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeLog(tc.in)
			if got != tc.want {
				t.Errorf("SanitizeLog(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "eyJhbGci") || strings.Contains(got, "dXNlcjpwYXNz") || strings.Contains(got, "PRIVATE KEY") {
				t.Errorf("输出不应残留敏感特征: %q", got)
			}
		})
	}
}

func TestSanitizeLogIsIdempotent(t *testing.T) {
	in := "Authorization: Bearer abc password=xyz"
	once := SanitizeLog(in)
	twice := SanitizeLog(once)
	if once != twice {
		t.Errorf("SanitizeLog 应幂等: %q != %q", once, twice)
	}
}

// TestSanitizeLogJSONFields 结构化 JSON 中的认证头/口令字段同样脱敏（WS 推送/结构化日志）。
func TestSanitizeLogJSONFields(t *testing.T) {
	in := `{"Authorization":"Bearer s3cr3t","x-api-key":"k-123","password":"p@ss","note":"ok"}`
	got := SanitizeLog(in)
	for _, leaked := range []string{"s3cr3t", "k-123", "p@ss"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("JSON 字段未脱敏（含 %q）: %s", leaked, got)
		}
	}
	if !strings.Contains(got, `"note":"ok"`) {
		t.Fatalf("非敏感字段不应被改写: %s", got)
	}
	if once, twice := SanitizeLog(in), SanitizeLog(SanitizeLog(in)); once != twice {
		t.Fatalf("JSON 脱敏应幂等: %q != %q", once, twice)
	}
}
