// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/model"
)

func TestOneShotRunAtSchema(t *testing.T) {
	schema := buildSchema(TaskParams{})
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema properties have type %T, want map[string]interface{}", schema["properties"])
	}

	runAt, ok := properties["run_at"].(map[string]interface{})
	if !ok {
		t.Fatalf("run_at property has type %T, want map[string]interface{}", properties["run_at"])
	}
	if runAt["type"] != "string" {
		t.Errorf("run_at schema type = %v, want string", runAt["type"])
	}
	if !strings.Contains(runAt["description"].(string), "RFC 3339") {
		t.Errorf("run_at schema description does not mention RFC 3339: %q", runAt["description"])
	}

	for _, field := range schema["required"].([]string) {
		if field == "run_at" {
			t.Error("run_at must be optional in the generated schema")
		}
	}

	if updateSchema := buildSchema(AITaskParams{}); updateSchema["properties"].(map[string]interface{})["run_at"] == nil {
		t.Error("update_task schema does not expose run_at")
	}
}

func TestAddOneShotTaskTypes(t *testing.T) {
	server := createAITestServer(t)
	runAtText := "2030-01-02T03:04:05.123456789+02:00"
	wantRunAt, err := time.Parse(time.RFC3339, runAtText)
	if err != nil {
		t.Fatalf("parse expected timestamp: %v", err)
	}
	wantRunAt = wantRunAt.UTC()

	shellResult, err := server.handleAddTask(context.Background(), makeRequest(t, TaskParams{
		Name:    "one-shot shell",
		Command: "echo shell",
		RunAt:   &runAtText,
		Enabled: true,
	}))
	if err != nil {
		t.Fatalf("add shell one-shot: %v", err)
	}
	var shellTask model.Task
	parseResponse(t, shellResult, &shellTask)
	assertOneShotTask(t, shellTask, wantRunAt)

	aiResult, err := server.handleAddAITask(context.Background(), makeRequest(t, AITaskParams{
		TaskParams: TaskParams{
			Name:    "one-shot AI",
			RunAt:   &runAtText,
			Enabled: true,
		},
		Prompt: "summarize this",
	}))
	if err != nil {
		t.Fatalf("add AI one-shot: %v", err)
	}
	var aiTask model.Task
	parseResponse(t, aiResult, &aiTask)
	assertOneShotTask(t, aiTask, wantRunAt)

	httpResult, err := server.handleAddHTTPTask(context.Background(), makeRequest(t, TaskParams{
		Name:    "one-shot HTTP",
		URL:     "https://example.com/hook",
		RunAt:   &runAtText,
		Enabled: true,
	}))
	if err != nil {
		t.Fatalf("add HTTP one-shot: %v", err)
	}
	var httpTask model.Task
	parseResponse(t, httpResult, &httpTask)
	assertOneShotTask(t, httpTask, wantRunAt)

	for _, task := range []*model.Task{&shellTask, &aiTask, &httpTask} {
		if task.Schedule != "" {
			t.Errorf("%s schedule = %q, want empty", task.Name, task.Schedule)
		}
		if task.RunAt == nil {
			continue
		}
		if task.RunAt.Location() != time.UTC {
			t.Errorf("%s RunAt location = %s, want UTC", task.Name, task.RunAt.Location())
		}
	}
}

func assertOneShotTask(t *testing.T, task model.Task, wantRunAt time.Time) {
	t.Helper()
	if task.RunAt == nil {
		t.Fatalf("%s RunAt is nil", task.Name)
	}
	if !task.RunAt.Equal(wantRunAt) {
		t.Errorf("%s RunAt = %v, want %v", task.Name, task.RunAt, wantRunAt)
	}
	if !task.NextRun.Equal(wantRunAt) {
		t.Errorf("%s NextRun = %v, want %v", task.Name, task.NextRun, wantRunAt)
	}
}

func TestOneShotTimestampValidation(t *testing.T) {
	tests := []struct {
		name string
		args interface{}
	}{
		{
			name: "malformed",
			args: TaskParams{Name: "bad", Command: "echo bad", RunAt: stringPointer("tomorrow"), Enabled: true},
		},
		{
			name: "relative",
			args: TaskParams{Name: "relative", Command: "echo bad", RunAt: stringPointer("10m"), Enabled: true},
		},
		{
			name: "schedule conflict",
			args: TaskParams{Name: "conflict", Command: "echo bad", Schedule: "@daily", RunAt: stringPointer("2030-01-02T03:04:05Z"), Enabled: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := createAITestServer(t)
			if result, err := server.handleAddTask(context.Background(), makeRequest(t, test.args)); err == nil {
				t.Fatalf("expected invalid request, got result %#v", result)
			} else if !strings.HasPrefix(err.Error(), "invalid input:") {
				t.Fatalf("error = %q, want invalid input error", err)
			}
		})
	}

	server := createAITestServer(t)
	past := "2000-01-02T03:04:05Z"
	result, err := server.handleAddTask(context.Background(), makeRequest(t, TaskParams{
		Name:    "past one-shot",
		Command: "echo past",
		RunAt:   &past,
		Enabled: true,
	}))
	if err != nil {
		t.Fatalf("past timestamp should be accepted: %v", err)
	}
	var task model.Task
	parseResponse(t, result, &task)
	if task.RunAt == nil || !task.NextRun.Equal(*task.RunAt) {
		t.Fatalf("past one-shot = %#v, want RunAt and NextRun set", task)
	}
}

func TestOneShotUpdateOmissionNullAndMergedConflict(t *testing.T) {
	server := createAITestServer(t)
	runAtText := "2030-01-02T03:04:05Z"
	task := mustAddShellTask(t, server, TaskParams{
		Name:    "update one-shot",
		Command: "echo update",
		RunAt:   &runAtText,
		Enabled: true,
	})

	updatedResult, err := server.handleUpdateTask(context.Background(), makeRequest(t, map[string]interface{}{
		"id":   task.ID,
		"name": "updated one-shot",
	}))
	if err != nil {
		t.Fatalf("update without run_at: %v", err)
	}
	parseResponse(t, updatedResult, &task)
	if task.RunAt == nil || !task.NextRun.Equal(*task.RunAt) {
		t.Fatalf("omitted run_at update = %#v, want existing RunAt and NextRun preserved", task)
	}

	clearedResult, err := server.handleUpdateTask(context.Background(), makeRequest(t, map[string]interface{}{
		"id":     task.ID,
		"run_at": nil,
	}))
	if err != nil {
		t.Fatalf("clear run_at: %v", err)
	}
	task = model.Task{}
	parseResponse(t, clearedResult, &task)
	if task.RunAt != nil || !task.NextRun.IsZero() {
		t.Fatalf("null run_at update = %#v, want one-shot cleared", task)
	}

	newRunAtText := "2031-02-03T04:05:06+01:00"
	rearmedResult, err := server.handleUpdateTask(context.Background(), makeRequest(t, map[string]interface{}{
		"id":      task.ID,
		"run_at":  newRunAtText,
		"enabled": true,
	}))
	if err != nil {
		t.Fatalf("replace run_at: %v", err)
	}
	parseResponse(t, rearmedResult, &task)
	wantRunAt, _ := time.Parse(time.RFC3339, newRunAtText)
	wantRunAt = wantRunAt.UTC()
	if task.RunAt == nil || !task.RunAt.Equal(wantRunAt) || !task.NextRun.Equal(wantRunAt) {
		t.Fatalf("replacement run_at = %#v, want %v armed", task, wantRunAt)
	}

	conflictResult, err := server.handleUpdateTask(context.Background(), makeRequest(t, map[string]interface{}{
		"id":       task.ID,
		"schedule": "@daily",
	}))
	if err == nil || conflictResult != nil {
		t.Fatalf("merged schedule/run_at update = result %#v, error %v; want invalid input", conflictResult, err)
	}
	if !strings.HasPrefix(err.Error(), "invalid input:") {
		t.Fatalf("merged conflict error = %q, want invalid input error", err)
	}

	unchanged, err := server.scheduler.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task after rejected update: %v", err)
	}
	if unchanged.Schedule != "" || unchanged.RunAt == nil || !unchanged.NextRun.Equal(wantRunAt) {
		t.Fatalf("task after rejected update = %#v, want unchanged one-shot", unchanged)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestDisabledOneShotHasNoDueTime(t *testing.T) {
	srv := createAITestServer(t)
	runAt := "2030-01-02T03:04:05Z"
	task := mustAddShellTask(t, srv, TaskParams{Name: "disabled", Command: "echo once", RunAt: &runAt})
	if task.Enabled || task.RunAt == nil || !task.NextRun.IsZero() {
		t.Fatalf("disabled one-shot has unexpected state: %+v", task)
	}
	if err := srv.scheduler.EnableTask(task.ID); err != nil {
		t.Fatal(err)
	}
	enabled, err := srv.scheduler.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.NextRun.Equal(*enabled.RunAt) {
		t.Fatalf("enabled one-shot not armed: %+v", enabled)
	}
}
