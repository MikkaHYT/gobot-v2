package helpers

import "time"

var DefaultRadioCoverURL = "https://i.ibb.co/ZzWQXNJJ/lostcover.png"

const (
	DurationFeedbackShort = 3 * time.Second
	DurationFeedbackLong  = 6 * time.Second
	DurationPagination    = 120 * time.Second
	DurationTimeoutHTTP   = 10 * time.Second
)
