package ui

import (
	"errors"
	"fmt"
)

var errSentinel = errors.New("sentinel")

func msgTypeName(m any) string {
	return fmt.Sprintf("%T", m)
}
