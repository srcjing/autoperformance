package domain

import "testing"

// TestEnvStatusMachine 环境状态机：手动仅 pending ↔ disabled；
// testing 的进入/退出由任务编排驱动（Phase 4），手动流转一律拒绝。
func TestEnvStatusMachine(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{EnvStatusPending, EnvStatusDisabled, true},
		{EnvStatusDisabled, EnvStatusPending, true},
		{EnvStatusPending, EnvStatusPending, false},
		{EnvStatusDisabled, EnvStatusDisabled, false},
		{EnvStatusPending, EnvStatusTesting, false},
		{EnvStatusTesting, EnvStatusPending, false},
		{EnvStatusTesting, EnvStatusDisabled, false},
		{"unknown", EnvStatusPending, false},
		{EnvStatusPending, "unknown", false},
	}
	for _, c := range cases {
		if got := CanTransitEnvStatus(c.from, c.to); got != c.want {
			t.Errorf("CanTransitEnvStatus(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}

	if !CanMarkEnvTesting(EnvStatusPending) || CanMarkEnvTesting(EnvStatusDisabled) {
		t.Fatal("仅 pending 可被任务占用（pending → testing）")
	}
	if !CanReleaseEnvTesting(EnvStatusTesting) || CanReleaseEnvTesting(EnvStatusPending) {
		t.Fatal("仅 testing 可释放占用（testing → pending）")
	}
}

// TestEnvEnums 环境状态/类型枚举校验。
func TestEnvEnums(t *testing.T) {
	for _, s := range []string{EnvStatusPending, EnvStatusTesting, EnvStatusDisabled} {
		if !IsValidEnvStatus(s) {
			t.Errorf("状态 %s 应合法", s)
		}
	}
	if IsValidEnvStatus("offline") {
		t.Error("offline 不应合法")
	}
	if !IsValidEnvType(EnvTypeSingle) || !IsValidEnvType(EnvTypeCluster) {
		t.Error("single/cluster 应合法")
	}
	if IsValidEnvType("vm") {
		t.Error("vm 不应合法")
	}
}
