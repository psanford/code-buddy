package interactive

import (
	"bufio"
	"context"
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

var rawSystemPrompt = `You are a 10x software engineer with exceptional problem-solving skills, attention to detail, and a deep understanding of software design principles. You will be given a question or task about a software project. Your job is to answer or solve that task while adhering to best practices and considering code quality, performance, security, and maintainability.

Your first task is to devise a plan for how you will solve this task. Generate a list of steps to perform. You can revise this list later as you learn new things along the way.

Generate all of the relevant information necessary to pass along to another software engineering assistant so that it can pick up and perform the next step in the instructions. That assistant will have no additional context besides what you provide so be sure to include all relevant information necessary to perform the next step.

<context>
project=%s
first 10 files in project:
%s
file_count=%s
</context>

In this environment, you can invoke tools using the following syntax:
#function_call,function,$FUNCTION_NAME
#function_call,parameter,$PARAM_NAME
$PARAM_VALUE
#function_call,end_parameter
#function_call,end_function
#function_call,invoke

Each #function_call directive must be at the start of a new line. You should stop after each function call invokation to allow me to run the function and return the results to you.

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
	APIKey               string
	Model                string
	OverrideSystemPrompt *string
	DebugLogger          *slog.Logger
}

func (r *Runner) Run(ctx context.Context) error {

	var (
		turns        []claude.MessageTurn
		multiline    bool
		systemPrompt string

		project = inferProject()
		stdin   = bufio.NewReader(os.Stdin)
		client  = anthropic.NewClient(r.APIKey)
	)

	if r.OverrideSystemPrompt != nil {
		systemPrompt = *r.OverrideSystemPrompt
	} else {
		var (
			rgFiles    = "?"
			fileCountS = "?"
			fileCount  = -1
		)
		if strings.HasSuffix(project, ".git") {
			rgOut, err := exec.Command("rg", "--files").CombinedOutput()
			if err != nil {
				return err
			}
			rgFileLines := strings.Split(string(rgOut), "\n")
			fileCount = len(rgFileLines)
			if fileCount > 10 {
				rgFileLines = rgFileLines[:9]
			}
			rgFiles = strings.Join(rgFileLines, "\n")
		}

		funCallReversed := reverseString("function_call")
		fixedSystemPrompt := strings.ReplaceAll(rawSystemPrompt, "function_call", funCallReversed)

		if fileCount > -1 {
			fileCountS = strconv.Itoa(fileCount)
		}

		systemPrompt = fmt.Sprintf(fixedSystemPrompt, project, rgFiles, fileCountS)
	}

	rl := readlinePrompt()
	defer rl.Close()

OUTER:
	for {
		var promptLines []string
		for i, readMoreLines := 0, true; readMoreLines; i++ {
			readMoreLines = multiline

			if i == 0 {
				rl.SetPrompt("prompt> ")
			} else {
				rl.SetPrompt("prompt» ")
			}

			promptLine, err := rl.Readline()
			if err == readline.ErrInterrupt { // ctrl-c
				break OUTER
			} else if err == io.EOF {
				if multiline {
					readMoreLines = false
					break
				} else {
					break OUTER
				}
			} else if err != nil {
				return err
			}

			promptLines = append(promptLines, promptLine)
			if i == 0 && strings.HasPrefix(promptLine, "/") {
				break
			}
		}

		userPrompt := strings.TrimSpace(strings.Join(promptLines, "\n"))
		if strings.HasPrefix(userPrompt, "/") {
			switch userPrompt {
			case "/help":
				helpMsg()
			case "/reset":
				turns = []claude.MessageTurn{}
			case "/multiline":
				multiline = !multiline
				fmt.Printf("multiline=%t\n", multiline)
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

		stopSeq := commandPrefix + ",invoke"

		req := &claude.MessageRequest{
			Model:         r.Model,
			Stream:        true,
			System:        systemPrompt,
			StopSequences: []string{stopSeq},
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

				functionCall, err := parseCommand(blk.Text)
				if err == io.EOF {
					continue
				} else if err != nil {
					return err
				}

				paramMap := make(map[string]string)
				for _, p := range functionCall.Parameters {
					paramMap[p.Name] = string(p.Value)
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
/multiline  - enable multi-line mode Ctrl-d to send
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
		readline.PcItem("/multiline"),
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
	Name       string              `xml:"name,attr"`
	Parameters []FunctionParameter `xml:"parameter"`
}

type FunctionParameter struct {
	Name  string
	Value string
}

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
