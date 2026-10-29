package testing

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// TestTemplateCRUD 模板增删改查与基于模板创建任务的默认值复用（AC-TEST-1）。
func TestTemplateCRUD(t *testing.T) {
	repo := &fakeTaskRepo{}
	svc := NewTaskService(repo, nil)

	in := CreateTemplateInput{
		Name:         "下单链路基线",
		EnvIDs:       []int64{1, 2},
		InterfaceIDs: []int64{11, 12},
		Flow:         json.RawMessage(`{"concurrency_mode":"fixed","target_vus":50,"duration":"30s"}`),
		ReportConfig: json.RawMessage(`{"formats":["html"]}`),
		CleanConfig:  json.RawMessage(`{"actions":["clean_data"]}`),
		Description:  "基线模板",
	}
	created, err := svc.CreateTemplate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Version != 1 {
		t.Fatalf("模板创建结果异常: %+v", created)
	}

	got, err := svc.GetTemplate(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != in.Name || len(got.InterfaceIDs) != 2 {
		t.Fatalf("模板详情异常: %+v", got)
	}

	in.Name = "下单链路基线 v2"
	in.Version = created.Version
	updated, err := svc.UpdateTemplate(context.Background(), created.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("版本应自增，got %d", updated.Version)
	}

	// 版本冲突 → 10402。
	if _, err := svc.UpdateTemplate(context.Background(), created.ID, CreateTemplateInput{
		Name: "x", Flow: in.Flow, ReportConfig: in.ReportConfig, CleanConfig: in.CleanConfig,
		Version: created.Version,
	}); !errors.Is(err, errcode.ErrConcurrent) {
		t.Fatalf("旧版本更新应 10402, got %v", err)
	}

	list, err := svc.ListTemplates(context.Background(), ListTemplatesQuery{Keyword: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 {
		t.Fatalf("关键字筛选结果异常: %+v", list)
	}

	if err := svc.DeleteTemplate(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetTemplate(context.Background(), created.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("删除后查询应 23005, got %v", err)
	}
}

// TestTemplateValidation 模板必填项与配置白名单校验。
func TestTemplateValidation(t *testing.T) {
	svc := NewTaskService(&fakeTaskRepo{}, nil)

	if _, err := svc.CreateTemplate(context.Background(), CreateTemplateInput{
		Name: " ", Flow: json.RawMessage(`{}`), ReportConfig: json.RawMessage(`{}`), CleanConfig: json.RawMessage(`{}`),
	}); !errors.Is(err, errcode.ErrParamMiss) {
		t.Fatalf("名称缺失应 10002, got %v", err)
	}

	if _, err := svc.CreateTemplate(context.Background(), CreateTemplateInput{
		Name: "模板", Flow: json.RawMessage(`{"target_vus":"1;exec('id')"}`),
		ReportConfig: json.RawMessage(`{}`), CleanConfig: json.RawMessage(`{}`),
	}); !errors.Is(err, ErrConcurrencyInvalid) {
		t.Fatalf("模板流程并发数非法应 23004, got %v", err)
	}
}

// TestCreateTaskFromTemplate 基于模板创建：请求未显式给出的要素沿用模板（PRD §3.3.2）。
func TestCreateTaskFromTemplate(t *testing.T) {
	repo := &fakeTaskRepo{envStatus: domain.EnvStatusPending}
	svc := NewTaskService(repo, nil)

	tpl, err := svc.CreateTemplate(context.Background(), CreateTemplateInput{
		Name:         "模板",
		EnvIDs:       []int64{9},
		InterfaceIDs: []int64{21, 22},
		Flow:         json.RawMessage(`{"concurrency_mode":"fixed","target_vus":10,"duration":"10s"}`),
		ReportConfig: json.RawMessage(`{"formats":["html"]}`),
		CleanConfig:  json.RawMessage(`{"actions":["clean_data"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	task, err := svc.Create(context.Background(), CreateTaskInput{
		Name:       "基于模板",
		TemplateID: tpl.ID,
		TargetHost: "10.20.30.40",
		CreatedBy:  7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.EnvID != 9 || len(task.InterfaceSnapshots) != 2 {
		t.Fatalf("模板默认值未生效: %+v", task)
	}
	if len(task.ThresholdSnapshots) == 0 {
		t.Fatal("基于模板创建同样需要阈值快照")
	}
}
