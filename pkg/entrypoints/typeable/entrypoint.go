package typeable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wrouesnel/ctxstdio"
	"github.com/wrouesnel/typeable/version"

	"github.com/alecthomas/kong"
)

// Name is the name of this subapplication.
const Name = "typeable"

// Exit codes returned by Entrypoint.
const (
	ExitOK    = 0
	ExitError = 1
	// ExitCancelled is returned when decoding is cancelled, following the shell
	// convention for a command interrupted by Ctrl-C.
	ExitCancelled = 130
)

// CLIConfig is the root command line configuration parsed by kong.
type CLIConfig struct {
	Version kong.VersionFlag `help:"Show version number"`

	Encode EncodeCmd `cmd:"" help:"Encode stdin as a grid to be retyped"`
	Decode DecodeCmd `cmd:"" help:"Decode a retyped grid from stdin"`
}

// kongExit carries an exit code requested by kong (e.g. for --help or --version) out of the
// parser so Entrypoint can return it instead of the process exiting.
type kongExit struct {
	code int
}

// Entrypoint is the real application entrypoint. This structure allows test packages to E2E-style tests invoking commmands
// as though they are on the command line, but using built-in coverage tools. Stub-main under the `cmd` package calls this
// function with os.Args[1:].
func Entrypoint(ctx context.Context, args []string) (exitCode int) {
	stdOut := ctxstdio.StdOut(ctx)
	stdErr := ctxstdio.StdErr(ctx)

	defer func() {
		if r := recover(); r != nil {
			exit, ok := r.(kongExit)
			if !ok {
				panic(r)
			}
			exitCode = exit.code
		}
	}()

	appCtx, appCancel := context.WithCancel(ctx)
	defer appCancel()

	// Command line parsing can now happen
	vars := kong.Vars{
		"version": version.Version,
	}

	// cli is the root entrypoint config parsed by kong and made available to subcommands via
	// a binding.
	var cli CLIConfig

	parser, err := kong.New(&cli,
		kong.Name(Name),
		kong.Description(version.Description),
		kong.DefaultEnvars(version.Name),
		kong.Writers(stdOut, stdErr),
		kong.Exit(func(code int) { panic(kongExit{code: code}) }),
		vars)
	if err != nil {
		_, _ = fmt.Fprintf(stdErr, "Failure while building command line parser: %v\n", err)
		return 1
	}

	kongCtx, err := parser.Parse(args)
	parser.FatalIfErrorf(err)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	// Stop signal delivery on return so repeated calls (e.g. from tests) don't leak handlers.
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()
	sigCtx, cancelFn := context.WithCancel(appCtx)
	defer cancelFn()
	go func() {
		for range sigCh {
			cancelFn()
		}
	}()

	// Main application context
	kongCtx.BindTo(sigCtx, (*context.Context)(nil))
	// Main command line
	kongCtx.Bind(&cli)
	// kongCtx if needed.
	kongCtx.Bind(kongCtx)

	// Command errors are reported as a plain message, since typeable is used interactively
	// and from scripts. Cancelling is silent, but has its own exit code.
	runErr := kongCtx.Run()
	switch {
	case errors.Is(runErr, ErrCancelled) || errors.Is(runErr, context.Canceled):
		return ExitCancelled
	case runErr != nil:
		_, _ = fmt.Fprintf(stdErr, "%s: %v\n", Name, runErr)
		return ExitError
	}
	return ExitOK
}
