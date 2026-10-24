package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func confirmPoolAction(in *os.File, out io.Writer, text string) error {
	if !term.IsTerminal(int(in.Fd())) {
		return fmt.Errorf("pool policy changes require an interactive terminal and typed confirmation")
	}
	if _, err := fmt.Fprintln(out, text+"\nType APPROVE to continue:"); err != nil {
		return fmt.Errorf("write pool confirmation: %w", err)
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read pool confirmation: %w", err)
	}
	if strings.TrimSpace(line) != "APPROVE" {
		return fmt.Errorf("pool action canceled")
	}
	return nil
}
