package precheck

import "context"

func (i *Inspector) sbxBinary() Check {
	return i.executable("sbx", "sbx", minSBXVersion, func(ctx context.Context) (string, error) {
		return i.sandboxes.Version(ctx)
	})
}
