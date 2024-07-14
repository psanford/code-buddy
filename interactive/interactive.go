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

	rl := readlinePrompt()
	defer rl.Close()

OUTER:
	for {

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
			cmd := strings.SplitN(userPrompt, " ", 2)[0]
			switch cmd {
			case "/help":
				helpMsg()
			case "/reset":
				turns = []claude.MessageTurn{}
			case "/multiline":
				multiline = !multiline
				fmt.Printf("multiline=%t\n", multiline)
			case "/model":
				parts := strings.SplitN(userPrompt, " ", 2)
				if len(parts) > 1 {
					modelName := parts[1]
					fmt.Printf("set model=%s\n", modelName)
					r.Model = modelName
				} else {
					fmt.Printf("model=%s\n", r.Model)
				}
			case "/system":
				newSystemPrompt := strings.TrimSpace(strings.TrimPrefix(userPrompt, "/system"))
				if newSystemPrompt != "" {
					if newSystemPrompt == "RESET" {
						r.OverrideSystemPrompt = nil
						fmt.Println("reset system prompt back to default")
					} else {
						r.OverrideSystemPrompt = &newSystemPrompt
						fmt.Printf("set system_prompt=%s\n", newSystemPrompt)
					}
				} else {
					if r.OverrideSystemPrompt != nil {
						fmt.Printf("system_prompt=%s\n", *r.OverrideSystemPrompt)
					} else {
						fmt.Printf("system_prompt=%s\n", systemPrompt)
					}
				}
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

		model := r.Model
		if fullModel := humanModelNameMap[model]; fullModel != "" {
			model = fullModel
		}

		req := &claude.MessageRequest{
			Model:         model,
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
				case "append_to_file":
					cmd = &AppendToFileArgs{
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
/help							- show this help message
/reset						- clear all history and start again
/multiline				- enable multi-line mode Ctrl-d to send
/model <model>		- get/set model
/system <prompt>	- get/set system prompt (RESET to reset)
/history					- show full conversation history
/quit							- exit program`)
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
		readline.PcItem("/model",
			readline.PcItemDynamic(func(line string) []string {
				return []string{"sonnet", "haiku", "opus"}
			}),
		),
		readline.PcItem("/system"),
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

var humanModelNameMap = map[string]string{
	"haiku":  claude.Claude3Haiku,
	"sonnet": claude.Claude3Dot5Sonnet,
	"opus":   claude.Claude3Opus,
}
