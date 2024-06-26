package accumulator

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
