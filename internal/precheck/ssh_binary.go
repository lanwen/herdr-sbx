package precheck

import (
	"context"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

func (i *Inspector) sshBinary() Check {
	return i.executable("OpenSSH", "ssh", "", func(ctx context.Context) (string, error) {
		return i.commands.CombinedOutput(ctx, process.Command{Name: "ssh", Args: []string{"-V"}, Timeout: 5 * time.Second})
	})
}
