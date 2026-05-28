package ui

import "time"

type imageStateMsg struct {
	Name  string
	State string
	When  time.Time
}

type imageNameMsg struct {
	Name      string
	ImageName string
}

type imageOutputMsg struct {
	Name string
	Line string
}

type imageDoneMsg struct {
	Name string
	Err  error
}

type allDoneMsg struct{}

type tickMsg struct {
	Now time.Time
}
