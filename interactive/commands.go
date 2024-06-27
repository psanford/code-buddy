package interactive

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"

	"github.com/psanford/claude"
)

type ListFilesArgs struct {
	Pattern string `json:"pattern"`
}

func (a *ListFilesArgs) PrettyCommand() string {
	return fmt.Sprintf("rg --files | rg %s", a.Pattern)
}

var cmdCombinedOutput = func(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).CombinedOutput()
}

func (a *ListFilesArgs) Run() (string, error) {
	regx, err := regexp.Compile("(?m)" + a.Pattern)
	if err != nil {
		return "", err
	}

	cmdOut, err := cmdCombinedOutput("rg", "--files")
	if err != nil {
		return "", err
	}

	var outBuf bytes.Buffer
	r := bufio.NewReader(bytes.NewBuffer(cmdOut))
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}

		if regx.Match(line) {
			outBuf.Write(line)
		}
	}

	return outBuf.String(), nil
}

type RGArgs struct {
	Pattern   string `json:"pattern"`
	Directory string `json:"directory"`
}

func (a *RGArgs) PrettyCommand() string {
	return fmt.Sprintf("rg %s %s", a.Pattern, a.Directory)
}

func (a *RGArgs) Run() (string, error) {
	cmdOut, err := cmdCombinedOutput("rg", a.Pattern, a.Directory)
	if err != nil {
		return "", err
	}

	return string(cmdOut), nil
}

type CatArgs struct {
	Filename string `json:"filename"`
}

func (a *CatArgs) Run() (string, error) {
	b, err := os.ReadFile(a.Filename)
	return string(b), err
}

func (a *CatArgs) PrettyCommand() string {
	return fmt.Sprintf("cat %s", a.Filename)
}

type ModifyFileArgs struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

func (a *ModifyFileArgs) Run() (string, error) {
	err := os.WriteFile(a.Filename, []byte(a.Content), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("File %s has been modified successfully.", a.Filename), nil
}

func (a *ModifyFileArgs) PrettyCommand() string {
	return fmt.Sprintf("cat > %s <<-EOF\n%s\n\nEOF\n# destination: %s", a.Filename, a.Content, a.Filename)
}

var tools = []claude.Tool{
	{
		Name:        "list_files",
		Description: "List files in the project. The list of files can be filtered by providing a regular expression to this function. This is equivalent to running `rg --files | rg $pattern`",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]struct {
				Description string `json:"description"`
				Type        string `json:"type"`
			}{
				"pattern": {
					Description: "The ripgrep regex pattern to filter files",
					Type:        "string",
				},
			},
			Required: []string{"pattern"},
		},
	},
	{
		Name:        "rg",
		Description: "rg (ripgrep) is a tool for recursively searching for lines matching a regex pattern.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]struct {
				Description string `json:"description"`
				Type        string `json:"type"`
			}{
				"pattern": {
					Description: "The regex pattern to search for",
					Type:        "string",
				},
				"directory": {
					Description: "The directory to search in",
					Type:        "string",
				},
			},
			Required: []string{"pattern", "directory"},
		},
	},
	{
		Name:        "cat",
		Description: "Read the contents of a file",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]struct {
				Description string `json:"description"`
				Type        string `json:"type"`
			}{
				"filename": {
					Description: "The name of the file to read",
					Type:        "string",
				},
			},
			Required: []string{"filename"},
		},
	},
	{
		Name:        "modify_file",
		Description: "Modify the contents of a file. You MUST provide the full contents of the file!",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]struct {
				Description string `json:"description"`
				Type        string `json:"type"`
			}{
				"filename": {
					Description: "The name of the file to modify",
					Type:        "string",
				},
				"content": {
					Description: "The new content to write to the file",
					Type:        "string",
				},
			},
			Required: []string{"filename", "content"},
		},
	},
}
