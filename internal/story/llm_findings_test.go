// Ver 2026-08-16 23:25, by gemini-3.7-flash

package story

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// mockLLMServer creates a test HTTP server that responds with canned assistant content.
func mockLLMServer(t *testing.T, cannedResponse string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			http.NotFound(w, r)
			return
		}
		resp := chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: cannedResponse}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestParseJSONFromLLM(t *testing.T) {
	t.Run("clean object", func(t *testing.T) {
		type Sample struct {
			Name string `json:"name"`
		}
		var s Sample
		err := parseJSONFromLLM(`{"name":"test"}`, &s)
		if err != nil || s.Name != "test" {
			t.Fatalf("expected name=test, got err=%v, s=%+v", err, s)
		}
	})

	t.Run("wrapped in markdown fences with leading text", func(t *testing.T) {
		type Item struct {
			ID int `json:"id"`
		}
		var items []Item
		raw := "Here is the result:\n```json\n[{\"id\":1},{\"id\":2}]\n```\nHope it helps!"
		err := parseJSONFromLLM(raw, &items)
		if err != nil || len(items) != 2 || items[0].ID != 1 {
			t.Fatalf("failed to parse fenced json array: err=%v, items=%+v", err, items)
		}
	})
}

func TestP1b1_ToolResultMisinterpretation(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	// Step 1: tool call read_file
	// Step 2: request has tool_result "404 Not Found", reasoning claims "Successfully read config"
	r1 := audit.Record{
		TS: at(0), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "load config")},
			}},
			Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"config.json\"}"}}]}}]}
data: [DONE]`},
		},
	}
	r2 := audit.Record{
		TS: at(1), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{
					msg("user", "load config"),
					map[string]any{
						"role": "assistant",
						"tool_calls": []any{
							map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"config.json"}`}},
						},
					},
					map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "Error: 404 file not found"},
				},
			}},
			Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Done!","reasoning_content":"Successfully read config.json, proceeding to start service."}}]}
data: [DONE]`},
		},
	}

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	canned := `[
  {
    "step_seq": 1,
    "is_misinterpreted": true,
    "confidence": "HIGH",
    "evidence_anchor": "Tool: 404 file not found vs Reasoning: Successfully read config.json",
    "explanation": "工具返回404错误，模型推理却声称成功读取",
    "suggested_action": "检查文件读取逻辑"
  }
]`
	srv := mockLLMServer(t, canned)
	defer srv.Close()

	opts := LLMOptions{Addr: srv.Listener.Addr().String(), Model: "agent"}
	findings := detectLLMToolResultMisinterpretation(context.Background(), j, opts, i18n.ZH)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Code != FindingToolResultMisinterpretation {
		t.Errorf("Code = %s, want %s", f.Code, FindingToolResultMisinterpretation)
	}
	if f.Source != SourceLLMInferred || f.Confidence != ConfidenceHigh {
		t.Errorf("Source=%s, Confidence=%s, want llm_inferred/HIGH", f.Source, f.Confidence)
	}
	if f.EvidenceAnchor == "" {
		t.Errorf("EvidenceAnchor should not be empty")
	}
}

func TestP1b2_SemanticOscillation(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	var recs []audit.Record
	queries := []string{"fix bug", "how to fix bug", "resolve bug issue"}
	for i, q := range queries {
		recs = append(recs, audit.Record{
			TS: at(i), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
					"model": "agent", "messages": []any{msg("user", "search")},
				}},
				Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_` + string(rune('a'+i)) + `","type":"function","function":{"name":"search","arguments":"{\"query\":\"` + q + `\"}"}}]}}]}
data: [DONE]`},
			},
		})
	}

	path := writeJSONL(t, recs)
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	canned := `[
  {
    "step_seq": 3,
    "is_oscillating": true,
    "confidence": "HIGH",
    "evidence_anchor": "连续3次调用search工具使用同义词查询（'fix bug', 'how to fix bug', 'resolve bug issue'）",
    "explanation": "模型陷入同义词无效搜索死循环",
    "suggested_breakout": "建议切换探索方向"
  }
]`
	srv := mockLLMServer(t, canned)
	defer srv.Close()

	opts := LLMOptions{Addr: srv.Listener.Addr().String(), Model: "agent"}
	findings := detectLLMSemanticOscillation(context.Background(), j, opts, i18n.ZH)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Code != FindingSemanticOscillation {
		t.Errorf("expected FindingSemanticOscillation, got %s", findings[0].Code)
	}
}

func TestP1b3_GoalDrift(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	var recs []audit.Record
	var msgs []any
	msgs = append(msgs, msg("user", "Fix auth login bug in router.go"))
	recs = append(recs, audit.Record{
		TS: at(0), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": msgs,
			}},
			Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Starting bug fix."}}]}
data: [DONE]`},
		},
	})
	msgs = append(msgs, msg("assistant", "Starting bug fix."))
	for i := 1; i <= 8; i++ {
		msgs = append(msgs, msg("user", "continue"))
		recs = append(recs, audit.Record{
			TS: at(i), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
					"model": "agent", "messages": msgs,
				}},
				Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Refactoring Makefile."}}]}
data: [DONE]`},
			},
		})
		msgs = append(msgs, msg("assistant", "Refactoring Makefile."))
	}

	path := writeJSONL(t, recs)
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	canned := `{
  "drift_detected": true,
  "drift_step_seq": 4,
  "confidence": "HIGH",
  "evidence_anchor": "初始目标为Fix auth login bug，中间步骤持续进行Refactoring Makefile",
  "drift_explanation": "偏离了核心Bug修复目标，陷入Makefile重构",
  "suggested_action": "重新聚焦初始用户目标"
}`
	srv := mockLLMServer(t, canned)
	defer srv.Close()

	opts := LLMOptions{Addr: srv.Listener.Addr().String(), Model: "agent"}
	findings := detectLLMGoalDrift(context.Background(), j, opts, i18n.ZH)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Code != FindingGoalDrift {
		t.Errorf("expected FindingGoalDrift, got %s", findings[0].Code)
	}
}

func TestP1b4_CompactionConstraintDropped(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	sys := msg("system", "Strict Rule: Never modify schema.sql under any circumstances.")
	u1 := msg("user", "start task")

	predMsgs := []any{sys, u1}
	var recs []audit.Record
	for i := 0; i < 5; i++ {
		recs = append(recs, mkRecWithUsage(at(i), predMsgs, "ok", 1000+int64(i)*100, 50))
		predMsgs = append(predMsgs, msg("assistant", "step done"))
	}

	succMsgs := []any{msg("system", "sys v2"), u1, msg("assistant", "resuming without rules")}
	recs = append(recs, mkRecWithUsage(at(30), succMsgs, "continuing", 500, 20))

	path := writeJSONL(t, recs)
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	ctxgraph.StitchGraph(g)
	byIdx := ctxgraph.LineageIndex(g)
	if len(g.Lineages) < 2 {
		t.Fatalf("expected at least 2 lineages, got %d", len(g.Lineages))
	}
	second := g.Lineages[1]
	chain := ctxgraph.ChainFrom(second, byIdx)

	j, err := BuildChain(chain, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("BuildChain: %v", err)
	}

	canned := `[
  {
    "step_seq": 6,
    "constraint_lost": true,
    "confidence": "HIGH",
    "evidence_anchor": "Strict Rule: Never modify schema.sql under any circumstances.",
    "explanation": "上下文压缩丢弃了关键否定式安全约束",
    "suggested_action": "在后续对话中重新注入该核心约束"
  }
]`
	srv := mockLLMServer(t, canned)
	defer srv.Close()

	opts := LLMOptions{Addr: srv.Listener.Addr().String(), Model: "agent"}
	findings := detectLLMConstraintDropped(context.Background(), j, opts, i18n.ZH)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Code != FindingConstraintTextDropped {
		t.Errorf("expected FindingConstraintTextDropped, got %s", findings[0].Code)
	}
}

func TestP1b5_PlanExecutionMisalignment(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	planText := "计划如下：\n1. 修改代码\n2. 运行单元测试\n3. 提交更改"
	ssePlan, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"index": 0,
				"delta": map[string]any{
					"role":    "assistant",
					"content": planText,
				},
			},
		},
	})
	ssePlanBody := "data: " + string(ssePlan) + "\ndata: [DONE]\n"

	r1 := audit.Record{
		TS: at(0), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "fix bug")},
			}},
			Response: &audit.Message{Status: 200, Body: ssePlanBody},
		},
	}
	r2 := audit.Record{
		TS: at(1), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "fix bug"), msg("assistant", planText), msg("user", "proceed")},
			}},
			Response: &audit.Message{Status: 200, Body: sseText("Done all steps!")},
		},
	}

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	canned := `{
  "has_misalignment": true,
  "unfulfilled_items": [{"seq": 2, "text": "运行单元测试", "status": "UNFULFILLED"}],
  "confidence": "HIGH",
  "evidence_anchor": "计划第2条'运行单元测试'在后续执行中完全无对应动作",
  "explanation": "单元测试条目未被执行",
  "suggested_action": "补充运行单元测试"
}`
	srv := mockLLMServer(t, canned)
	defer srv.Close()

	opts := LLMOptions{Addr: srv.Listener.Addr().String(), Model: "agent"}
	findings := detectLLMPlanMisalignment(context.Background(), j, opts, i18n.ZH)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Code != FindingPlanExecutionMisalignment {
		t.Errorf("expected FindingPlanExecutionMisalignment, got %s", findings[0].Code)
	}
}

// TestP1b5_PlanExecutionMisalignment_DynamicReplan is the plan document's
// own negative calibration case (§3.1 P1b.5 negative example): the agent
// legitimately abandons its first plan for a substantially different one,
// and the audit must judge fulfillment against the CURRENT plan, not flag
// the first plan's now-irrelevant items as unfulfilled.
func TestP1b5_PlanExecutionMisalignment_DynamicReplan(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	plan1 := "计划如下：\n1. 修改代码\n2. 运行单元测试\n3. 提交更改"
	plan2 := "情况有变，新的计划：\n1. 排查数据库连接超时\n2. 更新连接池配置\n3. 编写压力测试脚本\n4. 通知运维团队"

	sseFor := func(text string) string {
		raw, _ := json.Marshal(map[string]any{
			"choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}},
			},
		})
		return "data: " + string(raw) + "\ndata: [DONE]\n"
	}

	r1 := audit.Record{
		TS: at(0), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "fix db timeout")},
			}},
			Response: &audit.Message{Status: 200, Body: sseFor(plan1)},
		},
	}
	r2 := audit.Record{
		TS: at(1), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "fix db timeout"), msg("assistant", plan1), msg("user", "change of plans")},
			}},
			Response: &audit.Message{Status: 200, Body: sseFor(plan2)},
		},
	}
	r3 := audit.Record{
		TS: at(2), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{
					msg("user", "fix db timeout"), msg("assistant", plan1), msg("user", "change of plans"),
					msg("assistant", plan2), msg("user", "proceed"),
				},
			}},
			Response: &audit.Message{Status: 200, Body: sseText("Done!")},
		},
	}

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		capturedBody = string(data)
		resp := chatCompletionResponse{Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Content string `json:"content"`
		}{Content: `{"has_misalignment":false,"unfulfilled_items":[],"confidence":"HIGH","evidence_anchor":"","explanation":"revised plan fully executed"}`}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	opts := LLMOptions{Addr: srv.Listener.Addr().String(), Model: "agent"}
	_ = detectLLMPlanMisalignment(context.Background(), j, opts, i18n.ZH)

	// Pull just the plan_items array out of the sent request — actions_taken
	// legitimately quotes the whole transcript (including the abandoned
	// plan's announcement turn), so the assertion must be scoped to what's
	// actually being audited, not the full request body.
	var req chatCompletionRequest
	if err := json.Unmarshal([]byte(capturedBody), &req); err != nil {
		t.Fatalf("unmarshal captured request: %v", err)
	}
	userContent := req.Messages[1].Content
	jsonStart := strings.Index(userContent, "{")
	jsonEnd := strings.LastIndex(userContent, "}")
	var pack PlanAuditEvidencePack
	if err := json.Unmarshal([]byte(userContent[jsonStart:jsonEnd+1]), &pack); err != nil {
		t.Fatalf("unmarshal evidence pack: %v", err)
	}

	if len(pack.PlanItems) != 4 {
		t.Fatalf("expected the revised 4-item plan, got %d items: %+v", len(pack.PlanItems), pack.PlanItems)
	}
	for _, it := range pack.PlanItems {
		if strings.Contains(it.Text, "运行单元测试") || strings.Contains(it.Text, "提交更改") {
			t.Errorf("plan_items still contains an abandoned first-plan item: %+v", pack.PlanItems)
		}
	}
}

func TestP1b6_UnverifiedCompletionClaim(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	r1 := audit.Record{
		TS: at(0), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "modify code and ensure it works")},
			}},
			Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"已成功修复所有相关逻辑并完成修改，所有功能均已验证通过！"}}]}
data: [DONE]`},
		},
	}

	path := writeJSONL(t, []audit.Record{r1})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	canned := `{
  "claim_status": "CLAIM_WITHOUT_VERIFICATION",
  "confidence": "HIGH",
  "evidence_anchor": "已成功修复所有相关逻辑并完成修改，所有功能均已验证通过！",
  "missing_verification": "全程未执行任何测试、编译或检查命令",
  "suggested_action": "在宣称完成前执行单元测试"
}`
	srv := mockLLMServer(t, canned)
	defer srv.Close()

	opts := LLMOptions{Addr: srv.Listener.Addr().String(), Model: "agent"}
	findings := detectLLMUnverifiedCompletionClaim(context.Background(), j, opts, i18n.ZH)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Code != FindingUnverifiedCompletionClaim {
		t.Errorf("expected FindingUnverifiedCompletionClaim, got %s", findings[0].Code)
	}
}

func TestComputeLLMFindings_EndToEndAndFailOpen(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	r1 := audit.Record{
		TS: at(0), DurMS: 100, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "test")},
			}},
			Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"reply"}}]}
data: [DONE]`},
		},
	}
	path := writeJSONL(t, []audit.Record{r1})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	t.Run("disabled opts returns nil", func(t *testing.T) {
		res, err := ComputeLLMFindings(context.Background(), j, LLMOptions{}, i18n.ZH)
		if err != nil || len(res) != 0 {
			t.Fatalf("expected nil findings with disabled opts, got %v, %v", res, err)
		}
	})

	t.Run("unreachable addr fails open without error", func(t *testing.T) {
		opts := LLMOptions{Addr: "127.0.0.1:59999", Model: "agent"}
		res, err := ComputeLLMFindings(context.Background(), j, opts, i18n.ZH)
		if err != nil {
			t.Fatalf("fail-open contract violated: expected nil error, got %v", err)
		}
		if len(res) != 0 {
			t.Fatalf("expected 0 findings on network failure, got %d", len(res))
		}
	})
}

func TestRenderSpine_InferredFindingRendering(t *testing.T) {
	finding := Finding{
		Code:           FindingToolResultMisinterpretation,
		StepSeq:        2,
		Source:         SourceLLMInferred,
		Confidence:     ConfidenceHigh,
		EvidenceAnchor: "404 Not Found vs Success",
		Finding:        "疑似工具结果曲解",
		Evidence:       "模型误把404当成成功",
		Action:         "人工复核",
	}

	var sb strings.Builder
	w := func(format string, args ...any) {
		sb.WriteString(fmt.Sprintf(format, args...))
	}

	renderFindingsSection(w, []Finding{finding}, i18n.ZH)
	rendered := sb.String()

	if !strings.Contains(rendered, "[AI推测 · 置信度: HIGH]") {
		t.Errorf("rendered output missing AI推测 badge:\n%s", rendered)
	}
	if !strings.Contains(rendered, "原文证据锚点：") || !strings.Contains(rendered, "404 Not Found vs Success") {
		t.Errorf("rendered output missing evidence anchor:\n%s", rendered)
	}
}

// TestRenderSpine_MixedSourceSameCode covers the case P1b.5 introduced: the
// rule detector (detectPlanExecutionMisalignment) and the LLM detector
// (detectLLMPlanMisalignment) can both independently fire
// FindingPlanExecutionMisalignment for the same Journey. The two entries
// must read as distinct verdicts, not a duplicate — the rule-sourced one
// picks up an explicit [规则检测] tag (normally left bare) precisely because
// an LLM-sourced sibling with the same Code is present.
func TestRenderSpine_MixedSourceSameCode(t *testing.T) {
	ruleFinding := Finding{
		Code:    FindingPlanExecutionMisalignment,
		StepSeq: 1,
		Finding: "规则检测：计划条目未见后续执行",
	}
	llmFinding := Finding{
		Code:           FindingPlanExecutionMisalignment,
		StepSeq:        4,
		Source:         SourceLLMInferred,
		Confidence:     ConfidenceHigh,
		EvidenceAnchor: "Plan: 2. 运行单元测试 vs Action: none",
		Finding:        "AI 推测：计划条目未见后续执行",
	}

	var sb strings.Builder
	w := func(format string, args ...any) {
		sb.WriteString(fmt.Sprintf(format, args...))
	}
	renderFindingsSection(w, []Finding{ruleFinding, llmFinding}, i18n.ZH)
	rendered := sb.String()

	if !strings.Contains(rendered, "[规则检测]") {
		t.Errorf("expected the rule-sourced entry to be tagged [规则检测] when an LLM-sourced sibling shares its Code:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[AI推测 · 置信度: HIGH]") {
		t.Errorf("expected the LLM-sourced entry to keep its existing badge:\n%s", rendered)
	}

	// A single-source hit (no sibling with the other Source) must NOT pick
	// up the [规则检测] tag — it stays bare, as before this change.
	sb.Reset()
	renderFindingsSection(w, []Finding{ruleFinding}, i18n.ZH)
	if strings.Contains(sb.String(), "[规则检测]") {
		t.Errorf("a lone rule finding (no LLM sibling) should not be tagged:\n%s", sb.String())
	}
}

func TestDetectOscillationCandidates_Deterministic(t *testing.T) {
	// Construct a step series where multiple tools oscillate in the same window.
	var steps []*Step
	for seq := 1; seq <= 6; seq++ {
		steps = append(steps, &Step{
			Seq: seq,
			ToolCalls: []chatmsg.ToolCall{
				{Name: "tool_b", Args: fmt.Sprintf(`{"arg": %d}`, seq)},
				{Name: "tool_a", Args: fmt.Sprintf(`{"arg": %d}`, seq)},
				{Name: "tool_c", Args: fmt.Sprintf(`{"arg": %d}`, seq)},
			},
		})
	}

	var firstRun []OscillationCandidate
	for i := 0; i < 200; i++ {
		cands := detectOscillationCandidates(steps)
		if i == 0 {
			firstRun = cands
			if len(firstRun) == 0 {
				t.Fatalf("expected candidates in first run, got 0")
			}
			continue
		}
		if len(cands) != len(firstRun) {
			t.Fatalf("run %d: expected %d candidates, got %d", i, len(firstRun), len(cands))
		}
		for k := range cands {
			if cands[k].ToolName != firstRun[k].ToolName || cands[k].Calls[0].StepSeq != firstRun[k].Calls[0].StepSeq {
				t.Fatalf("run %d candidate %d mismatch: got %+v, want %+v", i, k, cands[k], firstRun[k])
			}
		}
	}
}
