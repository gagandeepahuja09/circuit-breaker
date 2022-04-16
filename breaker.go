package circuitbreaker

import (
	"errors"
	"sync/atomic"
)

// Question: Why is atomic package required for state here?
// Answer: We are allowing other goroutines to check this value via State method.

const (
	CloseState uint32 = iota
	OpenState
)

var (
	ErrBreakerOpen = errors.New("BreakerOpen: error executing the function due to circuit breaker being open")
)

type Options struct {
	ErrorsPercentage       float64
	MinimumNumberOfRequest int64
	NumberOfSecondsToStore int
}

func OptionsDefault() Options {
	return Options{
		ErrorsPercentage:       50.0,
		MinimumNumberOfRequest: 20,
		NumberOfSecondsToStore: 10,
	}
}

type Breaker struct {
	state        uint32
	healthCounts *HealthCounts

	options Options

	// channel to get the changes in the breaker state
	changes chan uint32
}

func NewBreaker(opt Options) (*Breaker, error) {
	var err error
	b := &Breaker{
		state:   CloseState,
		options: opt,
		changes: make(chan uint32),
	}
	b.healthCounts, err = NewHealthCounts(opt.NumberOfSecondsToStore)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (b *Breaker) Call(fn func() error) error {
	state := atomic.LoadUint32(&b.state)

	if state == OpenState && b.update() == OpenState {
		return ErrBreakerOpen
	}

	err := fn()

	if err != nil {
		go b.fail()
	} else {
		go b.success()
	}
	return err
}

func (b *Breaker) Health() HealthSummary {
	return b.healthCounts.Summary()
}

func (b *Breaker) State() uint32 {
	return atomic.LoadUint32(&b.state)
}

func (b *Breaker) GetOptions() Options {
	return b.options
}

func (b *Breaker) Cancel() {
	b.healthCounts.Cancel()
}

func (b *Breaker) success() {
	b.healthCounts.Success()
	b.update()
}

func (b *Breaker) fail() {
	b.healthCounts.Fail()
	b.update()
}

func (b *Breaker) checkState() uint32 {
	hs := b.healthCounts.Summary()
	if hs.Total < b.options.MinimumNumberOfRequest {
		return CloseState
	}

	if hs.ErrorPercentage >= b.options.ErrorsPercentage {
		return OpenState
	}

	return CloseState
}

// returns the new state
func (b *Breaker) update() uint32 {
	state := atomic.LoadUint32(&b.state)
	newState := b.checkState()
	if state == newState {
		return state
	}

	changed := atomic.CompareAndSwapUint32(&b.state, state, newState)
	if changed {
		// non-blocking send, so that it doesn't slow down if no reader is available
		select {
		case b.changes <- newState:
		default:
		}
		return newState
	}
	return state
}
