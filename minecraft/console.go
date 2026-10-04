package minecraft

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
)

func (inst *Instance) readConsole(r io.ReadCloser, done chan struct{}) {
	defer close(done)
	defer r.Close()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
	}

	err := scanner.Err()
	if err != nil {
		slog.Error("console read failed", "error", err)
		io.Copy(io.Discard, r)
	}
}
