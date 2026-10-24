package cli

import (
	"io"
	"os"
	"testing"
)

func TestPoolConfirmationRefusesNonTerminal(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if err := confirmPoolAction(reader, io.Discard, "scale up"); err == nil {
		t.Fatal("noninteractive approval accepted")
	}
}
