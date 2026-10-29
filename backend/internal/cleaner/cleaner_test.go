package cleaner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

// fakeRunner 记录远程调用，不建立真实 SSH 连接。
type fakeRunner struct {
	calls []string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, hostID int64, command string, args []string) (string, error) {
	f.calls = append(f.calls, command)
	if f.err != nil {
		return "", f.err
	}
	return "ok", nil
}

// TestCleanerRejectsNonAllowlistedScript 脚本哈希不在白名单 → 拒绝执行（DES-T-003 / TEST-T-003）。
func TestCleanerRejectsNonAllowlistedScript(t *testing.T) {
	runner := &fakeRunner{}
	// 白名单只包含另一段脚本的哈希。
	other := sha256.Sum256([]byte("echo safe"))
	c := New(runner, NewAllowlist([]int64{1}, []string{hex.EncodeToString(other[:])}))

	plan := &Plan{
		Actions: []Action{},
		Hosts:   []int64{1},
		Scripts: []Script{{HostID: 1, Path: "/opt/cleanup/wrong.sh", Content: "rm -rf /"}},
	}
	if _, err := c.Run(context.Background(), plan); !errors.Is(err, ErrScriptNotAllowlisted) {
		t.Fatalf("非白名单脚本应被拒绝, got %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("拒绝后不得产生远程调用: %v", runner.calls)
	}
}

// TestCleanerRejectsNonAllowlistedHost 主机不在白名单 → 拒绝（DES-T-003）。
func TestCleanerRejectsNonAllowlistedHost(t *testing.T) {
	runner := &fakeRunner{}
	c := New(runner, NewAllowlist([]int64{1}, nil))
	plan := &Plan{Actions: []Action{ActionCleanData}, Hosts: []int64{2}}
	if _, err := c.Run(context.Background(), plan); !errors.Is(err, ErrHostNotAllowlisted) {
		t.Fatalf("非白名单主机应被拒绝, got %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("拒绝后不得产生远程调用")
	}
}

// TestCleanerRunsBuiltinActions 内置动作走固定受控助手命令（无用户文本拼接）。
func TestCleanerRunsBuiltinActions(t *testing.T) {
	runner := &fakeRunner{}
	c := New(runner, NewAllowlist([]int64{1, 2}, nil))
	plan := &Plan{
		Actions: []Action{ActionRestoreConfig, ActionReleaseResource},
		Hosts:   []int64{1, 2},
	}
	res, err := c.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("内置动作执行失败: %v", err)
	}
	if len(res.Steps) != 4 || !res.OK() {
		t.Fatalf("步骤结果异常: %+v", res)
	}
	for _, call := range runner.calls {
		if call != cleanupHelperPath {
			t.Fatalf("内置动作必须走固定受控助手路径, got %q", call)
		}
	}
}

// TestCleanerAllowlistedScriptRuns 白名单脚本可执行（按内容哈希校验）。
func TestCleanerAllowlistedScriptRuns(t *testing.T) {
	content := "#!/bin/sh\necho cleanup\n"
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])

	runner := &fakeRunner{}
	c := New(runner, NewAllowlist([]int64{1}, []string{hash}))
	plan := &Plan{
		Hosts:   []int64{1},
		Scripts: []Script{{HostID: 1, Path: "/opt/cleanup/ok.sh", Content: content}},
	}
	if _, err := c.Run(context.Background(), plan); err != nil {
		t.Fatalf("白名单脚本应可执行: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "/opt/cleanup/ok.sh" {
		t.Fatalf("脚本调用异常: %v", runner.calls)
	}
}

// TestParsePlan 清理配置解析：动作白名单、脚本路径穿越/元字符拒绝。
func TestParsePlan(t *testing.T) {
	plan, err := ParsePlan(json.RawMessage(`{"actions":["clean_data","release_resource"],"scripts":[{"host_id":"1","path":"/opt/cleanup/x.sh","content":"echo ok"}]}`), []int64{1})
	if err != nil {
		t.Fatalf("合法配置解析失败: %v", err)
	}
	if len(plan.Actions) != 2 || len(plan.Scripts) != 1 || len(plan.Hosts) != 1 {
		t.Fatalf("解析结果异常: %+v", plan)
	}

	for _, raw := range []string{
		`{"actions":["drop_database"]}`,
		`{"actions":["script"]}`,
		`{"actions":["clean_data"],"scripts":[{"host_id":1,"path":"/opt/../etc/passwd"}]}`,
		`{"actions":["clean_data"],"scripts":[{"host_id":1,"path":"/opt/x.sh;id;"}]}`,
	} {
		if _, err := ParsePlan(json.RawMessage(raw), []int64{1}); err == nil {
			t.Fatalf("非法配置应被拒绝: %s", raw)
		}
	}

	// 无主机 + 内置动作 → 无需清理（no-op 成功，空计划）。
	if plan, err := ParsePlan(json.RawMessage(`{"actions":["clean_data"]}`), nil); err != nil {
		t.Fatalf("无主机内置动作应成功(no-op), got %v", err)
	} else if len(plan.Actions) != 0 {
		t.Fatalf("无主机内置动作应为空计划, got %v", plan.Actions)
	}
	// 无主机 + 脚本动作 → ErrNoHost（脚本必须指定可信主机）。
	if _, err := ParsePlan(json.RawMessage(`{"actions":["script"],"scripts":[{"host_id":1,"path":"/opt/x.sh"}]}`), nil); !errors.Is(err, ErrNoHost) {
		t.Fatalf("无主机的脚本动作应返回 ErrNoHost, got %v", err)
	}
}

// TestSSHRunnerRejectsUnsafeCommand SSH 执行器自身再校验命令/参数白名单（纵深防御）。
func TestSSHRunnerRejectsUnsafeCommand(t *testing.T) {
	r := NewSSHHostRunner(nil)
	for _, cmd := range []string{"sh -c id", "/opt/x.sh;id;", "rm -rf /"} {
		if _, err := r.Run(context.Background(), 1, cmd, nil); err == nil {
			t.Fatalf("非法命令应被拒绝: %q", cmd)
		}
	}
	if _, err := r.Run(context.Background(), 1, cleanupHelperPath, []string{"clean-data;id"}); err == nil {
		t.Fatal("非法参数应被拒绝")
	}
}
