package manage

import "context"

// Fixture adapter; production initialization commits through PlatformService.
func initializeApp(ctx context.Context, root, core, address, name string) error {
	app, err := initialApp(ctx, root, core, address, name)
	if err != nil {
		return err
	}
	return CommitApp(ctx, root, core, &app)
}
