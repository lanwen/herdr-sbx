package panel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/store"
)

func (s *Service) guestPlugins(reader *bufio.Reader, out io.Writer) {
	for {
		choices, err := s.registration.GuestPluginChoices(context.Background())
		if err != nil {
			fmt.Fprintln(out, "Guest plugin defaults:", err)
			waitForReturn(reader, out)
			return
		}
		fmt.Fprintln(out, "\nGuest plugin defaults (new plugins start OFF)")
		for i, choice := range choices {
			state := "OFF"
			if choice.Enabled {
				state = "ON"
			}
			note := ""
			if !choice.Installed {
				note = " (not installed locally)"
			}
			fmt.Fprintf(out, "%d  [%s] %s%s\n", i+1, state, choice.ID, note)
		}
		fmt.Fprintln(out, "The worker applies changes. OFF only stops provisioning; guest copies stay intact.")
		fmt.Fprint(out, "Number to toggle (Enter to return): ")
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			return
		}
		index, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || index < 1 || index > len(choices) {
			fmt.Fprintln(out, "Choose a number from the list.")
			continue
		}
		choice := choices[index-1]
		if err := store.SetGuestPluginDefault(choice.ID, !choice.Enabled, nil); err != nil {
			fmt.Fprintln(out, "Save failed:", err)
		}
	}
}
