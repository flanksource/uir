package uir

import "fmt"

// Warning reports a node an operation could not handle. It is returned to
// the caller rather than logged, so the model package stays free of a logger.
type Warning struct {
	Message string
	Node    Node
}

func (w Warning) String() string {
	return fmt.Sprintf("%s: %s (%T)", w.Message, w.Node.GetIdentifier(), w.Node)
}
