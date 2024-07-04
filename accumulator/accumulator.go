package accumulator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/psanford/claude"
	"github.com/psanford/claude/clientiface"
)

type Accumulator struct {
	client                clientiface.Client
	contentBlockDeltaChan chan ContentBlock
	debugLogger           *slog.Logger
}

func New(client clientiface.Client, options ...Option) *Accumulator {
	a := &Accumulator{
		client: client,
	}

	for _, opt := range options {
		opt.set(a)
	}

	return a
}

type Result struct {
	ContentBlocks []ContentBlock
	StopReason    string
	StopSequence  string
}

type ContentBlock struct {
	Text     string `json:"text"`
	Typ      string `json:"type"`
	Idx      int    `json:"-"`
	ToolName string `json:"name,omitempty"`
	ToolID   string `json:"id,omitempty"`
}

func (c *ContentBlock) Type() string {
	return c.Typ
}

func (c *ContentBlock) TextContent() string {
	return c.Text
}

func (a *Accumulator) Complete(ctx context.Context, req *claude.MessageRequest, options ...CompleteOption) (*claude.MessageStart, error) {
	req.Stream = true

	mr, err := a.client.Message(ctx, req)
	if err != nil {
		return nil, err
	}

	var opts completeOptions
	for _, opt := range options {
		opt.set(&opts)
	}

	if opts.contentBlockDeltaChan != nil {
		defer close(opts.contentBlockDeltaChan)
	}

	contentBlocks := make([]ContentBlock, 0, 2)

	var (
		contentType    string
		contentBuilder strings.Builder
		toolName       string
		toolID         string

		contentIdx = -1
	)

	var startMsg claude.MessageStart

	for resp := range mr.Responses() {
		if a.debugLogger != nil && a.debugLogger.Enabled(ctx, slog.LevelDebug) {
			mj, err := json.Marshal(resp)
			if err != nil {
				panic(err)
			}
			a.debugLogger.Debug("message response", "resp", mj)
		}
		switch ev := resp.Data.(type) {
		case *claude.MessagePing:
		case *claude.MessageStart:
			startMsg = *ev
		case *claude.ContentBlockStart:
			contentType = ev.ContentBlock.Type
			contentIdx = ev.Index
			toolName = ev.ContentBlock.Name
			toolID = ev.ContentBlock.ID
			contentBuilder.Write([]byte(ev.ContentBlock.Text))
		case *claude.ContentBlockDelta:
			contentBuilder.Write([]byte(ev.Delta.Text))
			contentBuilder.Write([]byte(ev.Delta.PartialJson))

			if opts.contentBlockDeltaChan != nil {
				var blk ContentBlock
				if ev.Delta.Text != "" {
					blk.Text = ev.Delta.Text
				} else {
					blk.Text = ev.Delta.PartialJson
				}
				opts.contentBlockDeltaChan <- blk
			}

		case *claude.ContentBlockStop:
			blk := ContentBlock{
				Text:     contentBuilder.String(),
				Typ:      contentType,
				Idx:      contentIdx,
				ToolName: toolName,
				ToolID:   toolID,
			}

			contentBlocks = append(contentBlocks, blk)

			contentType = ""
			contentIdx = -1
			contentBuilder = strings.Builder{}
			toolName = ""
		case *claude.MessageDelta:
			startMsg.StopReason = ev.Delta.StopReason
			startMsg.StopSequence = ev.Delta.StopSequence

			startMsg.Usage.OutputTokens = int(ev.Usage.OutputTokens)
		case *claude.MessageStop:
		case *claude.ClaudeError:
			return nil, ev
		case *claude.ClientError:
			return nil, ev
		case error:
			return nil, ev
		default:
			return nil, fmt.Errorf("unexpected message type: %T %+v", ev, ev)
		}
	}

	startMsg.Content = make([]claude.TurnContent, len(contentBlocks))
	for i, blk := range contentBlocks {
		blk := blk
		startMsg.Content[i] = &blk
	}

	return &startMsg, nil
}
