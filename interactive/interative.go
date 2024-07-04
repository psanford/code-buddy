package interactive

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
	"github.com/psanford/claude"
	"github.com/psanford/claude/anthropic"
	"github.com/psanford/code-buddy/accumulator"
)

var systemPrompt = `You are a 10x software engineer. You will be given a question or task about a software project. You job is to answer or solve that task.

Your first task is to devise a plan for how you will solve this task. Generate a list of steps to perform. You can revise this list later as you learn new things along the way.

Generate all of the relevant information necessary to pass along to another software engineering assistant so that it can pick up and perform the next step in the instructions. That assistant will have no additional context besides what you provide so be sure to include all relevant information necessary to perform the next step.
<context>project=%s</context>
`

func Run(ctx context.Context, apiKey, model string) error {

	var (
		turns []claude.MessageTurn

		project = inferProject()
		stdin   = bufio.NewReader(os.Stdin)
		client  = anthropic.NewClient(apiKey)
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

		req := &claude.MessageRequest{
			Model:  model,
			Stream: true,
			System: fmt.Sprintf(systemPrompt, project),
			Tools:  tools,
		}

		moreWork := true

		for moreWork {
			moreWork = false
			cbCh := make(chan accumulator.ContentBlock)

			acc := accumulator.New(client)

			go func() {
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

			turnContents := make([]claude.TurnContent, 0, len(respMeta.Content))

			for _, content := range respMeta.Content {
				blk := content.(*accumulator.ContentBlock)

				if blk.Type() == "tool_use" {
					var args Cmd
					switch blk.ToolName {
					case "list_files":
						args = &ListFilesArgs{}
					case "rg":
						args = &RGArgs{}
					case "cat":
						args = &CatArgs{}
					case "modify_file":
						args = &ModifyFileArgs{}
					default:
						return fmt.Errorf("unknown tool %s", blk.ToolName)
					}

					text := content.TextContent()
					err = json.Unmarshal([]byte(text), args)
					if err != nil {
						return fmt.Errorf("json unmarshal args for %s err: %s text:<%s>", blk.ToolName, err, text)
					}

					turnContents = append(turnContents, &claude.TurnContentToolUse{
						Typ:   blk.Typ,
						ID:    blk.ToolID,
						Name:  blk.ToolName,
						Input: args,
					})

				} else {
					turnContents = append(turnContents, content)
				}
			}

			turns = append(turns, claude.MessageTurn{
				Role:    "assistant",
				Content: turnContents,
			})

			for _, content := range turnContents {
				blk, ok := content.(*claude.TurnContentToolUse)

				if ok {
					cmd := blk.Input.(Cmd)

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
						return fmt.Errorf("Command not accepted, aborting")
					}

					cmdOut, err := cmd.Run()
					if err != nil {
						return err
					}

					fmt.Printf("\nOutput: %s\n\n", cmdOut)

					toolResp := claude.MessageTurn{
						Role: "user",
						Content: []claude.TurnContent{
							claude.ToolResultContent(blk.ID, cmdOut),
						},
					}

					turns = append(turns, toolResp)
					moreWork = true
				}
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
