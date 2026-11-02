package sbx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

// PrepareGuestShell keeps Docker's forwarded agent available to sandbox shells.
// Callers must first verify eligibility and a running sandbox; exec can start it.
func (c *Client) PrepareGuestShell(ctx context.Context, name string) error {
	output, err := c.cli(ctx, 15*time.Second, true, "exec", name, "--", "env", "-u", "BASH_ENV", "/bin/bash", "--noprofile", "--norc", "-c", guestShellSetup)
	if err != nil {
		if errors.Is(err, process.ErrTimeout) {
			err = fmt.Errorf("sandbox preparation %w when trying to update Bash startup files for Docker's forwarded SSH agent", err)
			if detail := strings.TrimSpace(output); detail != "" {
				return fmt.Errorf("%w: %s", err, detail)
			}
			return err
		}
		if detail := strings.TrimSpace(output); detail != "" {
			return fmt.Errorf("prepare sandbox shell: %w: %s", err, detail)
		}
		return fmt.Errorf("prepare sandbox shell: %w", err)
	}
	return nil
}

const guestShellSetup = `set -eu
umask 077
update_shell() {
 startup_file=$1
 if [ -L "$startup_file" ] || { [ -e "$startup_file" ] && [ ! -f "$startup_file" ]; }; then
  printf '%s\n' "shell startup file is not a regular file: $startup_file" >&2
  return 1
 fi
 original_file=/dev/null
 # Create a unique file beside the destination so mv can replace it atomically.
 temp_file=$(mktemp "$startup_file.herdr-sbx.XXXXXX")
 trap 'rm -f "$temp_file"' EXIT HUP INT TERM
 if [ -f "$startup_file" ]; then
  original_file=$startup_file
  cp -p "$startup_file" "$temp_file"
 fi
 # Remove only our previous block; reject incomplete or nested markers.
 awk '
 $0 == "# herdr-sbx SSH agent begin" { if (inside) exit 1; inside=1; next }
 $0 == "# herdr-sbx SSH agent end" { if (!inside) exit 1; inside=0; next }
 !inside { print }
 END { if (inside) exit 1 }
 ' "$original_file" > "$temp_file"
 cat >> "$temp_file" <<'HERDR_SBX_SSH_AGENT'
# herdr-sbx SSH agent begin
# Use Docker's forwarded agent when Herdr rejects its root-owned socket.
if [ -S /run/ssh-agent.sock ]; then
 export SSH_AUTH_SOCK=/run/ssh-agent.sock
fi
# herdr-sbx SSH agent end
HERDR_SBX_SSH_AGENT
 # Leave identical files untouched; otherwise publish the complete replacement.
 if [ -f "$startup_file" ] && cmp -s "$startup_file" "$temp_file"; then
  rm -f "$temp_file"
 else
  mv "$temp_file" "$startup_file"
 fi
 trap - EXIT HUP INT TERM
}
update_shell "$HOME/.bashrc"
# Bash reads only the first existing login file in this order.
for login in .bash_profile .bash_login .profile; do
 if [ -e "$HOME/$login" ] || [ -L "$HOME/$login" ]; then
  update_shell "$HOME/$login"
  exit 0
 fi
done
update_shell "$HOME/.profile"`
