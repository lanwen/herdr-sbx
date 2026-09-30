package precheck

import "context"

func (i *Inspector) herdrBinary() Check {
	return i.executable("Herdr", i.herdr.Executable(), minHerdrVersion, func(ctx context.Context) (string, error) {
		return i.herdr.Version(ctx)
	})
}
