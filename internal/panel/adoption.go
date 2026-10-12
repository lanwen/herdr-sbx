package panel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func (s *Service) adoptMachine(reader *bufio.Reader, out io.Writer) {
	choices, err := s.registration.AdoptionChoices(context.Background())
	if err != nil {
		fmt.Fprintln(out, "Cannot list adoption candidates:", err)
		waitForReturn(reader, out)
		return
	}
	if len(choices) == 0 {
		fmt.Fprintln(out, "No eligible machines. Adoption requires one existing machine with the sandbox SSH target, the default session, and no conflicting ownership.")
		waitForReturn(reader, out)
		return
	}
	fmt.Fprintln(out, "Adopt an existing machine without disconnecting it:")
	for i, choice := range choices {
		fmt.Fprintf(out, "%d  %s -> %s (%s, %s)\n", i+1, choice.Sandbox.Name, choice.Machine.Label, choice.Machine.ID, choice.Machine.Target)
	}
	fmt.Fprintln(out, "The plugin will manage the selected entry, including removing it when its sandbox is deleted.")
	for {
		fmt.Fprint(out, "Number to adopt (Enter to cancel): ")
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			return
		}
		index, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || index < 1 || index > len(choices) {
			fmt.Fprintln(out, "Choose a number from the list.")
			continue
		}
		if err := s.registration.Adopt(context.Background(), choices[index-1]); err != nil {
			fmt.Fprintln(out, "Adoption failed:", err)
		} else {
			fmt.Fprintln(out, "Adopted. The machine ID and connection are preserved. The worker or r will apply normal setup.")
		}
		waitForReturn(reader, out)
		return
	}
}
