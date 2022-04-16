package circuitbreaker

const (
	CloseState uint32 = iota
	OpenState
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
	state                 int
	errorThresholdPercent int
}
