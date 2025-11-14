package cProtocol

import (
	"testing"
)

func TestHigh(t *testing.T) {
	n := NewHead()
	n.SetCommand(126)
	n.SetConfig(1, 1, 0)
	t.Log(n.GetCommand())
	t.Log(n.GetCommandType())
	t.Log(n.GetDirection())
	t.Log(n.GetResponse())
	// n.SetCID(cid.NewBytesID(time.Now()))
	n.SetContentLength(12345678)
	t.Log(n.GetContentLength())
	t.Log(n.GetBytes())
	s, err := HeadFromBytes(n.GetBytes())
	if err != nil {
		t.Error(err)
	}
	t.Log(s.GetCommand())
	t.Log(s.GetCommandType())
	t.Log(s.GetDirection())
	t.Log(s.GetResponse())
	t.Log(s.GetContentLength())
	t.Log(s.GetBytes())
}
