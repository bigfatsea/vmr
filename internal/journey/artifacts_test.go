// Ver 2026-09-08, by pi (coding)

package journey

import (
	"strings"
	"testing"
	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
)

func TestExtractArtifacts_StructuredAndBash(t *testing.T) {
	j := &Journey{
		Tasks: []*Task{
			{
				Steps: []*Step{
					{
						Seq: 1,
						ToolCalls: []chatmsg.ToolCall{
							{
								Name: "read_file",
								Args: `{"path": "README.md"}`,
							},
						},
					},
					{
						Seq: 2,
						ToolCalls: []chatmsg.ToolCall{
							{
								Name: "edit_file",
								Args: `{"path": "src/main.go", "content": "package main"}`,
							},
						},
					},
					{
						Seq: 3,
						ToolCalls: []chatmsg.ToolCall{
							{
								Name: "bash",
								Args: `{"command": "echo 'test' > /tmp/output.log && rm old.txt"}`,
							},
						},
					},
					{
						Seq: 4,
						ToolCalls: []chatmsg.ToolCall{
							{
								Name: "write_file",
								Args: `{"path": "src/main.go", "content": "package main; func main(){}"}`,
							},
						},
					},
				},
			},
		},
	}

	artifacts := ExtractArtifacts(j)
	if len(artifacts) < 3 {
		t.Fatalf("expected at least 3 artifacts, got %d: %+v", len(artifacts), artifacts)
	}

	// 1. read_file should be captured with op edit
	// 2. src/main.go should have op write (escalated from edit), firstStep 2, count 2, heuristic false
	var mainGo *Artifact
	for i := range artifacts {
		if artifacts[i].Path == "src/main.go" {
			mainGo = &artifacts[i]
			break
		}
	}
	if mainGo == nil {
		t.Fatalf("src/main.go not found in artifacts: %+v", artifacts)
	}
	if mainGo.Op != ArtifactOpWrite {
		t.Errorf("src/main.go Op = %v, want write", mainGo.Op)
	}
	if mainGo.FirstStep != 2 {
		t.Errorf("src/main.go FirstStep = %d, want 2", mainGo.FirstStep)
	}
	if mainGo.Count != 2 {
		t.Errorf("src/main.go Count = %d, want 2", mainGo.Count)
	}
	if mainGo.Heuristic {
		t.Errorf("src/main.go Heuristic = true, want false")
	}

	// 3. check bash output.log
	var outLog *Artifact
	for i := range artifacts {
		if artifacts[i].Path == "/tmp/output.log" {
			outLog = &artifacts[i]
			break
		}
	}
	if outLog == nil {
		t.Errorf("/tmp/output.log not found in bash mutations: %+v", artifacts)
	} else if !outLog.Heuristic {
		t.Errorf("/tmp/output.log should be marked heuristic")
	}
}
func TestBuildVMArtifacts_Render(t *testing.T) {
	s := &JourneySummary{
		Artifacts: []Artifact{
			{Path: "src/main.go", Op: ArtifactOpWrite, FirstStep: 1, Count: 2, Heuristic: false},
			{Path: "/tmp/out.log", Op: ArtifactOpBash, FirstStep: 3, Count: 1, Heuristic: true},
		},
	}
	blocksZH := buildVMArtifacts(s, i18n.ZH)
	if len(blocksZH) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocksZH))
	}
	vmZH := &JourneyVM{Artifacts: blocksZH}
	renderedZH := SerializeJourneyVM(vmZH)
	if !strings.Contains(renderedZH, "## 触达文件与资产") || !strings.Contains(renderedZH, "`src/main.go`") || !strings.Contains(renderedZH, "Shell 启发式") {
		t.Errorf("renderedZH missing expected elements: %s", renderedZH)
	}

	blocksEN := buildVMArtifacts(s, i18n.EN)
	vmEN := &JourneyVM{Artifacts: blocksEN}
	renderedEN := SerializeJourneyVM(vmEN)
	if !strings.Contains(renderedEN, "## Touched Artifacts") || !strings.Contains(renderedEN, "`src/main.go`") || !strings.Contains(renderedEN, "Shell heuristic") {
		t.Errorf("renderedEN missing expected elements: %s", renderedEN)
	}
}

func TestLooksLikeFilePath(t *testing.T) {
	valid := []string{
		"a.out", "build.bin", "main.o", "program.exe",
		"src/main.go", "output.txt", "config.yaml", "README.md",
		"Makefile", "Dockerfile", "dist/binary",
	}
	for _, p := range valid {
		if !looksLikeFilePath(p) {
			t.Errorf("looksLikeFilePath(%q) = false, want true", p)
		}
	}

	invalid := []string{
		"", "1", "42", "None", "document.getElementById",
		"obj.field", "func()", "calc(x)",
	}
	for _, p := range invalid {
		if looksLikeFilePath(p) {
			t.Errorf("looksLikeFilePath(%q) = true, want false", p)
		}
	}
}
