package adapters

import "testing"

func TestExtractContentNested(t *testing.T) {
	msg := map[string]interface{}{
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "nested-hi"},
			},
		},
	}
	if extractContent(msg) != "nested-hi" {
		t.Fatalf("%q", extractContent(msg))
	}
	if extractContent(map[string]interface{}{"content": "top"}) != "top" {
		t.Fatal("top")
	}
}

func TestHasToolUseNestedBlocks(t *testing.T) {
	msg := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "tool_use", "name": "Bash", "input": map[string]interface{}{"command": "ls"}},
		},
	}
	if !hasToolUse(msg) {
		t.Fatal("tool")
	}
	if extractToolDescription(msg) == "" {
		t.Fatal("desc")
	}
}
