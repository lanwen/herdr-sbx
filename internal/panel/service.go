package panel

import (
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
)

// Service connects terminal presentation to shared operation services.
type Service struct {
	registration *registration.Service
	lifecycle    *lifecycle.Service
	sbx          *sbx.Client
}

// New builds the interactive surface around shared operation clients.
func New(registrations *registration.Service, lifecycleService *lifecycle.Service, sandboxClient *sbx.Client) *Service {
	registrations.SetCheckPrinter(PrintChecks)
	lifecycleService.SetCheckPrinter(PrintChecks)
	return &Service{registration: registrations, lifecycle: lifecycleService, sbx: sandboxClient}
}

// Run owns the terminal until the user closes the interface. The worker has an
// independent lifetime and is never stopped as a consequence of closing.
func (s *Service) Run(in io.Reader, out io.Writer) error {
	_, err := tea.NewProgram(newModel(s), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}
