package interactive

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/psanford/claude"
	"github.com/psanford/claude/anthropic"
	"github.com/psanford/code-buddy/accumulator"
	"github.com/spf13/cobra"
)

var (
	modelFlag string
)

func Command() *cobra.Command {
	cmd := cobra.Command{
		Use:   "interactive",
		Short: "run interactive mode",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()

			apiKey := os.Getenv("CLAUDE_API_KEY")
			if apiKey == "" {
				log.Fatalf("Must set environment variable CLAUDE_API_KEY")
			}

			err := Run(ctx, apiKey)
			if err != nil {
				log.Fatal(err)
			}
		},
	}

	cmd.Flags().StringVar(&modelFlag, "model", claude.Claude3Dot5Sonnet, "model")

	return &cmd
}

var systemPrompt0 = `You are a 10x software engineer. You will be given a question or task about a software project. You job is to answer or solve that task.

Your first task is to devise a plan for how you will solve this task. Generate a list of steps to perform. You can revise this list later as you learn new things along the way.

Generate all of the relevant information necessary to pass along to another software engineering assistant so that it can pick up and perform the next step in the instructions. That assistant will have no additional context besides what you provide so be sure to include all relevant information necessary to perform the next step.
`

func Run(ctx context.Context, apiKey string) error {
	client := anthropic.NewClient(apiKey)
	stdin := bufio.NewReader(os.Stdin)

	// get request from user
	var userPrompt string

	for {
		fmt.Printf("user prompt> ")
		os.Stdout.Sync()

		line, err := stdin.ReadString('\n')
		if err != nil {
			return fmt.Errorf("Error reading from stdin: %w\n", err)
		}
		line = strings.TrimSpace(line)
		if line != "" {
			userPrompt = line
			break
		}
	}

	project := inferProject()
	prompt := fmt.Sprintf("<context>project=%s</context><user_request>%s</user_request>", project, userPrompt)

	turns := []claude.MessageTurn{
		{
			Role: "user",
			Content: []claude.TurnContent{
				claude.TextContent(prompt),
			},
		},
	}

	req := &claude.MessageRequest{
		Model:  claude.Claude3Dot5Sonnet,
		Stream: true,
		System: systemPrompt0,
		Tools:  tools,
	}

	moreWork := true

	for moreWork {
		moreWork = false
		cbCh := make(chan accumulator.ContentBlock)

		acc := accumulator.New(client)

		go func() {
			for cb := range cbCh {
				fmt.Print(cb.Text)
				os.Stdout.Sync()
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
	return nil
}

func inferProject() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").CombinedOutput()
	if err == nil {
		return string(out)
	}
	cwd, _ := os.Getwd()
	return cwd
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

type ListFilesArgs struct {
	Pattern string `json:"pattern"`
}

func (a *ListFilesArgs) PrettyCommand() string {
	return fmt.Sprintf("rg --files | rg %s", a.Pattern)
}

func (a *ListFilesArgs) Run() (string, error) {
	regx, err := regexp.Compile(a.Pattern)
	if err != nil {
		return "", err
	}

	cmdOut, err := exec.Command("rg", "--files").CombinedOutput()
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
	cmdOut, err := exec.Command("rg", a.Pattern, a.Directory).CombinedOutput()
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

var tools = []claude.Tool{
	{
		Name:        "list_files",
		Description: "List files in the project. The list of files can be filtered by providing a regular expression to this function. This is equivelent to running `rg --files | rg $pattern`",
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
}
