package interactive

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chzyer/readline"
	"github.com/psanford/claude"
	"github.com/psanford/claude/anthropic"
	"github.com/psanford/code-buddy/accumulator"
)

var base64Text = `
In this environment, you can invoke tools using a "<function_call>" block like the following:
<function_call name="$FUNCTION_NAME>
<parameter name="$PARAM_NAME" encoding="text|base64">$PARAM_VALUE</parameter>
</function_call>

You should not send anything after a function_call so that I can invoke the function for you and return the results to you.

If $PARAM_VALUE might include any xml tags, it MUST be base64 encoded. Set the encoding="base64" in the parameter in
that case. Do NOT put a trailing newline at the end of $PARAM_VALUE before base64 encoding it.
`

var nonBase64Text = `
In this environment, you can invoke tools using a "<function_call>" block like the following:
<function_call name="$FUNCTION_NAME>
<parameter name="$PARAM_NAME">$PARAM_VALUE</parameter>
</function_call>

You should not send anything after a function_call so that I can invoke the function for you and return the results to you.

Do NOT put a trailing newline at the end of $PARAM_VALUE before base64 encoding it.
`

var systemPrompt0 = `You are a 10x software engineer with exceptional problem-solving skills, attention to detail, and a deep understanding of software design principles. You will be given a question or task about a software project. Your job is to answer or solve that task while adhering to best practices and considering code quality, performance, security, and maintainability.

Your first task is to devise a plan for how you will solve this task. Generate a list of steps to perform. You can revise this list later as you learn new things along the way.

Generate all of the relevant information necessary to pass along to another software engineering assistant so that it can pick up and perform the next step in the instructions. That assistant will have no additional context besides what you provide so be sure to include all relevant information necessary to perform the next step.

<context>project=%s</context>`

var systemPrompt1 = `
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
`

type Runner struct {
	APIKey      string
	Model       string
	DebugLogger *slog.Logger
	UseBase64   bool
}

func (r *Runner) Run(ctx context.Context) error {

	var (
		turns []claude.MessageTurn

		project = inferProject()
		stdin   = bufio.NewReader(os.Stdin)
		client  = anthropic.NewClient(r.APIKey)
	)

	rl := readlinePrompt()
	defer rl.Close()

	for {
		userPrompt, err := rl.Readline()
		if err == readline.ErrInterrupt {
			if len(userPrompt) == 0 {
				break
			} else {
				continue
			}
		} else if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		userPrompt = strings.TrimSpace(userPrompt)
		if strings.HasPrefix(userPrompt, "/") {
			switch userPrompt {
			case "/help":
				helpMsg()
			case "/reset":
				turns = []claude.MessageTurn{}
			case "/history":
				for _, turn := range turns {
					fmt.Printf("%+v\n", turn)
				}
			case "/quit":
				return nil
			default:
				fmt.Println("unknown command")
				helpMsg()
			}

			continue
		}

		turns = append(turns, claude.MessageTurn{
			Role: "user",
			Content: []claude.TurnContent{
				claude.TextContent(userPrompt),
			},
		})

		systemPrompt := systemPrompt0 + nonBase64Text + systemPrompt1
		if r.UseBase64 {
			systemPrompt = systemPrompt0 + base64Text + systemPrompt1
		}

		req := &claude.MessageRequest{
			Model:         r.Model,
			Stream:        true,
			System:        fmt.Sprintf(systemPrompt, project),
			StopSequences: []string{"</function_call>"},
		}

		moreWork := true

		for moreWork {
			moreWork = false
			cbCh := make(chan accumulator.ContentBlock)

			acc := accumulator.New(client, accumulator.WithDebugLogger(r.DebugLogger))

			waitOnText := make(chan struct{})

			go func() {
				defer close(waitOnText)

				var lastText string
				for cb := range cbCh {
					fmt.Print(cb.Text)
					lastText = cb.Text
					os.Stdout.Sync()
				}
				if !strings.HasSuffix(lastText, "\n") {
					fmt.Println()
				}
			}()

			req.Messages = turns

			respMeta, err := acc.Complete(ctx, req, accumulator.WithContentBlockDeltaChan(cbCh))
			if err != nil {
				return err
			}

			<-waitOnText

			turnContents := make([]claude.TurnContent, 0, len(respMeta.Content))

			var cmd Cmd

			for _, content := range respMeta.Content {
				turnContents = append(turnContents, content)
				blk := content.(*accumulator.ContentBlock)
				if r.DebugLogger != nil && r.DebugLogger.Enabled(ctx, slog.LevelDebug) {
					r.DebugLogger.Debug("content_block", "blk", blk)
				}

				if blk.Type() != "text" {
					continue
				}
				xmlStart := strings.Index(blk.Text, "<function_call ")
				if xmlStart < 0 {
					continue
				}

				xmlContent := blk.Text[xmlStart:]
				if !strings.HasSuffix(xmlContent, "</function_call>") {
					xmlContent = xmlContent + "</function_call>"
				}
				var functionCall FunctionCall
				err := xml.Unmarshal([]byte(xmlContent), &functionCall)
				if err != nil {
					return err
				}

				paramMap := make(map[string]string)
				for _, p := range functionCall.Parameters {
					if p.Encoding == "base64" {
						v, err := base64.StdEncoding.DecodeString(p.Value)
						if err != nil {
							return err
						}
						paramMap[p.Name] = string(v)
					} else {
						paramMap[p.Name] = string(p.Value)
					}
				}

				switch functionCall.Name {
				case "list_files":
					cmd = &ListFilesArgs{
						Pattern: paramMap["pattern"],
					}
				case "rg":
					cmd = &RGArgs{
						Pattern:   paramMap["pattern"],
						Directory: paramMap["directory"],
					}
				case "cat":
					cmd = &CatArgs{
						Filename: paramMap["filename"],
					}
				case "write_file":
					cmd = &ModifyFileArgs{
						Filename: paramMap["filename"],
						Content:  paramMap["content"],
					}
				case "replace_string_in_file":
					count, _ := strconv.Atoi(paramMap["count"])
					cmd = &ReplaceStringInFileArgs{
						Filename:       paramMap["filename"],
						OriginalString: paramMap["original_string"],
						NewString:      paramMap["new_string"],
						Count:          count,
					}
				default:
					return fmt.Errorf("unknown tool %s", blk.ToolName)
				}
			}

			turns = append(turns, claude.MessageTurn{
				Role:    "assistant",
				Content: turnContents,
			})

			if cmd != nil {
				fmt.Printf("\nRequest to run command:\n\n%s\n\n", cmd.PrettyCommand())
				fmt.Print("ok? (y/N):")
				os.Stdout.Sync()

				var acceptCmd bool
				line, err := stdin.ReadString('\n')
				if err != nil {
					return fmt.Errorf("Error reading from stdin: %w\n", err)
				}
				line = strings.TrimSpace(line)
				if line == "y" {
					acceptCmd = true
				}

				if !acceptCmd {
					fmt.Println("Command not accepted, aborting")
					moreWork = false
					break
				}

				var (
					stderr    string
					errorCode int
				)
				cmdOut, err := cmd.Run()
				if err != nil {
					fmt.Printf("\nCMD ERROR: %s\n", err)
					stderr = err.Error()
					errorCode = 1
				}

				fmt.Printf("\nOutput: %s\n\n", cmdOut)

				toolResp := claude.MessageTurn{
					Role: "user",
					Content: []claude.TurnContent{
						claude.TextContent(fmt.Sprintf(`<function_result>
<stdout>%s</stdout>
<stderr>%s</stderr>
<exit_code>%d</exit_code>
</function_result>`, cmdOut, stderr, errorCode)),
					},
				}

				turns = append(turns, toolResp)
				moreWork = true
			}
		}
	}
	return nil
}

type InputSchema struct {
	Properties map[string]struct {
		Description string `json:"description"`
		Type        string `json:"type"`
	} `json:"properties"`
	Required []string `json:"required"`
	Type     string   `json:"type"`
}

type Cmd interface {
	PrettyCommand() string
	Run() (string, error)
}

func inferProject() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").CombinedOutput()
	if err == nil {
		return string(out)
	}
	cwd, _ := os.Getwd()
	return cwd
}

func readUserPrompt(stdin *bufio.Reader) (string, error) {
	for {
		fmt.Printf("user prompt> ")
		os.Stdout.Sync()

		line, err := stdin.ReadString('\n')
		if err == io.EOF {
			return "", err
		} else if err != nil {
			return "", fmt.Errorf("Error reading from stdin: %w\n", err)
		}
		line = strings.TrimSpace(line)
		if line != "" {
			return line, nil
		}
	}
}

func helpMsg() {
	fmt.Println(`help
/help       - show this help message
/reset      - clear all history and start again
/history    - show full conversation history
/quit       - exit program`)
}

func readlinePrompt() *readline.Instance {
	cacheDirRoot, _ := os.UserCacheDir()
	if cacheDirRoot == "" {
		cacheDirRoot = filepath.Join(os.Getenv("HOME"), ".cache")
	}

	cacheDir := filepath.Join(cacheDirRoot, "code-buddy")
	os.MkdirAll(cacheDir, 0700)

	historyFile := filepath.Join(cacheDir, ".history")

	completer := readline.NewPrefixCompleter(
		readline.PcItem("/help"),
		readline.PcItem("/reset"),
		readline.PcItem("/history"),
		readline.PcItem("/quit"),
	)

	l, err := readline.NewEx(&readline.Config{
		Prompt:            "prompt> ",
		HistoryFile:       historyFile,
		AutoComplete:      completer,
		InterruptPrompt:   "^C",
		EOFPrompt:         "/quit",
		HistorySearchFold: true,
	})
	if err != nil {
		panic(err)
	}
	l.CaptureExitSignal()

	return l
}

type FunctionCall struct {
	XMLName    xml.Name            `xml:"function_call"`
	Name       string              `xml:"name,attr"`
	Parameters []FunctionParameter `xml:"parameter"`
}

type FunctionParameter struct {
	Name     string `xml:"name,attr"`
	Encoding string `xml:"encoding,attr"`
	Value    string `xml:",chardata"`
}
