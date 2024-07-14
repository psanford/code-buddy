package interactive

import (
	"bytes"
	"text/template"
)

type SystemPromptBuilder struct {
	Project             string
	FileCount           int
	FirstFilesInProject []string
	FunctionCallPrefix  string
}

func newSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		FileCount:          -1,
		FunctionCallPrefix: reverseString("function_call"),
	}
}

func (b *SystemPromptBuilder) String() string {
	var buf bytes.Buffer
	err := systemPromptTemplate.Execute(&buf, b)
	if err != nil {
		panic(err)
	}
	return buf.String()
}

var systemPromptTemplate = template.Must(template.New("").Parse(`Your first task is to devise a plan for how you will solve this task. Generate a list of steps to perform. You can revise this list later as you learn new things along the way.

Generate all of the relevant information necessary to pass along to another software engineering assistant so that it can pick up and perform the next step in the instructions. That assistant will have no additional context besides what you provide so be sure to include all relevant information necessary to perform the next step.

<context>
{{if not (eq .Project "")}}
project={{.Project}}
{{end}}
{{if gt (len .FirstFilesInProject) 0}}
first 10 files in project:
{{range .FirstFilesInProject}}
{{.}}
{{end}}
{{end}}
{{if gt .FileCount -1}}
file_count={{.FileCount}}
{{end}}
</context>

In this environment, you can invoke tools using the following syntax:
#{{.FunctionCallPrefix}},function,$FUNCTION_NAME
#{{.FunctionCallPrefix}},parameter,$PARAM_NAME
$PARAM_VALUE
#{{.FunctionCallPrefix}},end_parameter
#{{.FunctionCallPrefix}},end_function
#{{.FunctionCallPrefix}},invoke

Each #{{.FunctionCallPrefix}} directive must be at the start of a new line. You should stop after each function call invokation to allow me to run the function and return the results to you.

The response will be in the form:
<function_result>
<stdout>$STDOUT</stdout>
<stderr>$STDERR</stderr>
<exit_code>$EXIT_CODE</exit_code>
</function_result>

The available functions that you can invoke this way are:

<function name="write_file">
<parameter name="filename"/>
<parameter name="content"/>
<description>Modify the full contents of a file. You MUST provide the full contents of the file!</description>
</function>

<function name="append_to_file">
<parameter name="filename"/>
<parameter name="content"/>
<description>Append content to the end of a file.</description>
</function>

<function name="replace_string_in_file">
<parameter name="filename"/>
<parameter name="original_string"/>
<parameter name="new_string"/>
<parameter name="count"/>
<description>Partially modify the contents of a file. This works the same way as Go's string.Replace() function: Replace returns a copy of the string s with the first n non-overlapping instances of old replaced by new. If old is empty, it matches at the beginning of the string and after each UTF-8 sequence, yielding up to k+1 replacements for a k-rune string. If n < 0, there is no limit on the number of replacements.
You should prefer this function to write_file whenever you are making partial updates to a file.
</description>
</function

<function name="list_files">
<parameter name="pattern"/>
<description>List files in the project. The list of files can be filtered by providing a regular expression to this function. This is equivalent to running "rg --files | rg $pattern"</description>
</function>

<function name="rg">
<parameter name="pattern"/>
<parameter name="directory"/>
<description>rg (ripgrep) is a tool for recursively searching for lines matching a regex pattern.</description>
</function>


<function name="cat">
<parameter name="filename"/>
<description>Read the contents of a file</description>
</function>
`))

func reverseString(input string) string {
	rune := make([]rune, len(input))

	var n int
	for _, r := range input {
		rune[n] = r
		n++
	}
	rune = rune[0:n]

	for i := 0; i < n/2; i++ {
		rune[i], rune[n-1-i] = rune[n-1-i], rune[i]
	}

	return string(rune)
}
