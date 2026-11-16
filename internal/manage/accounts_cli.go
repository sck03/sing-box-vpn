package manage

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/sck03/sing-box-vpn/internal/application"
)

// RunAccounts accepts a typed JSON command from stdin to keep credentials and
// account settings out of the process command line and shell history.
func RunAccounts(ctx context.Context, args []string, input io.Reader, out io.Writer) error {
	f := flag.NewFlagSet("accounts", flag.ContinueOnError)
	root := f.String("root", "/etc/s-box", "configuration root")
	if err := f.Parse(args); err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(input, 64<<10))
	decoder.DisallowUnknownFields()
	var c application.AccountCommand
	if err := decoder.Decode(&c); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected one account command")
	}
	return (AccountService{Root: *root}).Execute(ctx, c, out)
}
