package accumulator

import "log/slog"

type Option interface {
	set(*Accumulator)
}

type completeOptions struct {
	contentBlockDeltaChan chan ContentBlock
}

type CompleteOption interface {
	set(*completeOptions)
}

type contentBlockDeltaChan struct {
	ch chan ContentBlock
}

func (c *contentBlockDeltaChan) set(a *completeOptions) {
	a.contentBlockDeltaChan = c.ch
}

func WithContentBlockDeltaChan(ch chan ContentBlock) CompleteOption {
	return &contentBlockDeltaChan{ch}
}

type debugLoggerOption struct {
	l *slog.Logger
}

func (o *debugLoggerOption) set(a *Accumulator) {
	a.debugLogger = o.l
}

func WithDebugLogger(l *slog.Logger) Option {
	return &debugLoggerOption{
		l: l,
	}
}
