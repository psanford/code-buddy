package interactive

import (
	"strings"
	"testing"
)

func TestSystemPromptBuilderString(t *testing.T) {
	tests := []struct {
		name       string
		builder    *SystemPromptBuilder
		expected   []string
		unexpected []string
	}{
		{
			name: "Filled Builder",
			builder: &SystemPromptBuilder{
				Project:             "test-project",
				FileCount:           5,
				FirstFilesInProject: []string{"file1.go", "file2.go"},
				FunctionCallPrefix:  "overlapped-acknowledges",
			},
			expected: []string{
				"project=test-project",
				"file_count=5",
				"file1.go",
				"file2.go",
				"#overlapped-acknowledges,function,$FUNCTION_NAME",
				"<function name=\"write_file\">",
				"<function name=\"append_to_file\">",
				"<function name=\"replace_string_in_file\">",
				"<function name=\"list_files\">",
				"<function name=\"rg\">",
				"<function name=\"cat\">",
			},
			unexpected: []string{
				"file_count=-1",
				"#function_call,function,$FUNCTION_NAME",
			},
		},
		{
			name: "Empty Builder",
			builder: &SystemPromptBuilder{
				FunctionCallPrefix: "overlapped-acknowledges",
				FileCount:          -1,
			},
			expected: []string{
				"#overlapped-acknowledges,function,$FUNCTION_NAME",
				"<function name=\"write_file\">",
				"<function name=\"append_to_file\">",
				"<function name=\"replace_string_in_file\">",
				"<function name=\"list_files\">",
				"<function name=\"rg\">",
				"<function name=\"cat\">",
			},
			unexpected: []string{
				"project=",
				"file_count=",
				"first 10 files in project:",
			},
		},
		{
			name: "Builder with FilesContent",
			builder: &SystemPromptBuilder{
				Project:             "test-project",
				FileCount:           3,
				FirstFilesInProject: []string{"file1.go", "file2.go", "file3.go"},
				FunctionCallPrefix:  "overlapped-acknowledges",
				FilesContent: []FileContent{
					{FileName: "file1.go", Content: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}"},
					{FileName: "file2.go", Content: "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}"},
				},
			},
			expected: []string{
				"<file>",
				"<filename>file1.go</filename>",
				"<filecontent>package main",
				"func main() {",
				"fmt.Println(\"Hello, World!\")",
				"<filename>file2.go</filename>",
				"<filecontent>package main",
				"func add(a, b int) int {",
				"return a + b",
			},
			unexpected: []string{
				"project=test-project",
				"file_count=3",
				"first 10 files in project:",
				"#overlapped-acknowledges,function,$FUNCTION_NAME",
				"<function name=\"write_file\">",
				"<function name=\"append_to_file\">",
				"<function name=\"replace_string_in_file\">",
				"<function name=\"list_files\">",
				"<function name=\"rg\">",
				"<function name=\"cat\">",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.builder.String()

			for _, element := range tt.expected {
				if !strings.Contains(result, element) {
					t.Errorf("Expected result to contain '%s', but it doesn't", element)
				}
			}

			for _, element := range tt.unexpected {
				if strings.Contains(result, element) {
					t.Errorf("Expected result not to contain '%s', but it does", element)
				}
			}
		})
	}
}
