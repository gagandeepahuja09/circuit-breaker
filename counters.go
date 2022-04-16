package circuitbreaker

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNumberOfSecondsToStoreOutOfBounds error = errors.New("NumberOfSecondsToStore out of bounds, should be between 1 and 60 seconds")
)

type HealthSummary struct {
	Failures        int64
	Success         int64
	Total           int64
	ErrorPercentage float64

	LastFailure time.Time
	LastSuccess time.Time
}

type HealthCountsBucket struct {
	failures  int64
	success   int64
	lastWrite time.Time
}

type HealthCounts struct {
	// buckets to store the counter
	values []HealthCountsBucket
	// number of buckets
	buckets int
	// time frame to store
	window time.Duration

	// time for the last event
	lastFailure time.Time
	lastSuccess time.Time

	// channels for the event loop
	successChan    chan struct{}
	failuresChan   chan struct{}
	summaryChan    chan struct{}
	summaryOutChan chan HealthSummary

	// context for cancelation
	ctx    context.Context
	cancel context.CancelFunc
}

func NewHealthCounts(numberOfSecondsToStore int) (*HealthCounts, error) {
	if numberOfSecondsToStore <= 0 || numberOfSecondsToStore > 60 {
		return nil, ErrNumberOfSecondsToStoreOutOfBounds
	}
	hc := &HealthCounts{
		buckets: numberOfSecondsToStore,
		window:  time.Duration(numberOfSecondsToStore) * time.Second,
		values:  make([]HealthCountsBucket, numberOfSecondsToStore),

		successChan:    make(chan struct{}),
		failuresChan:   make(chan struct{}),
		summaryChan:    make(chan struct{}),
		summaryOutChan: make(chan HealthSummary),
	}

	hc.ctx, hc.cancel = context.WithCancel(context.Background())

	go hc.run()
	return hc, nil
}

func (hc *HealthCounts) Fail() {
	hc.failuresChan <- struct{}{}
}

func (hc *HealthCounts) Success() {
	hc.successChan <- struct{}{}
}

func (hc *HealthCounts) Summary() HealthSummary {
	hc.summaryChan <- struct{}{}
	return <-hc.summaryOutChan
}

func (hc *HealthCounts) Cancel() {
	hc.cancel()
}

func (hc *HealthCounts) run() {
	for {
		select {
		case <-hc.successChan:
			hc.doSuccess()
		case <-hc.failuresChan:
			hc.doFail()
		case <-hc.summaryChan:
			hc.summaryOutChan <- hc.doSummary()
		case <-hc.ctx.Done():
			return
		}
	}
}

func (hc *HealthCounts) doSummary() HealthSummary {
	var hs HealthSummary

	now := time.Now()
	for _, value := range hc.values {
		// only consider if the last write for this bucket was within the window
		if !value.lastWrite.IsZero() && (now.Sub(value.lastWrite) <= hc.window) {
			hs.Success += value.success
			hs.Failures += value.failures
		}
	}
	hs.Total = hs.Success + hs.Failures
	if hs.Total == 0 {
		hs.ErrorPercentage = 0
	} else {
		hs.ErrorPercentage = float64(hs.Failures/hs.Total) * 100
	}

	hs.LastFailure = hc.lastFailure
	hs.LastSuccess = hc.lastSuccess
	return hs
}

func (hcb *HealthCountsBucket) reset() {
	hcb.failures = 0
	hcb.success = 0
}

// leaky bucket algorithm.
// bucket size = 5
// request at each second
// 1 -> 4, 2 -> 5, 3 -> 3, 4 -> 5, 5 -> 6
// 6 % 5 = 1. Have we seen a request at this index before? yes
// how much time has it elapsed. is it > the window size? yes.
// Then reset for that window. 1 -> 1.
func (hc *HealthCounts) bucket() *HealthCountsBucket {
	now := time.Now()
	index := now.Second() % hc.buckets
	if !hc.values[index].lastWrite.IsZero() {
		elapsed := now.Sub(hc.values[index].lastWrite)
		if elapsed > hc.window {
			hc.values[index].reset()
		}
	}
	hc.values[index].lastWrite = now
	return &hc.values[index]
}

func (hc *HealthCounts) doSuccess() {
	hc.bucket().success++
	hc.lastSuccess = time.Now()
}

func (hc *HealthCounts) doFail() {
	hc.bucket().failures++
	hc.lastFailure = time.Now()
}
