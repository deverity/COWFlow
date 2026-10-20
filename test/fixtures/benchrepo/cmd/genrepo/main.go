package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/deverity/cowflow/test/fixtures/benchrepo"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("genrepo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profile := fs.String("profile", "small", "profile: small, large or monorepo")
	seed := fs.Int64("seed", 1, "seed; the same seed gives the same repository")
	out := fs.String("out", "", "directory to create the repository in (missing or empty)")
	verify := fs.String("verify", "", "report bytes and files of HEAD of this existing repository")
	symlinks := fs.Bool("symlinks", false, "add symbolic links (Unix tests)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var (
		rep benchrepo.Report
		err error
	)
	switch {
	case *verify != "" && *out != "":
		_, _ = fmt.Fprintln(stderr, "genrepo: --verify and --out are mutually exclusive")
		return 2
	case *verify != "":
		rep, err = benchrepo.Inspect(ctx, *verify)
	case *out != "":
		var p benchrepo.Profile
		p, err = benchrepo.ProfileByName(*profile)
		if err == nil {
			p.Symlinks = *symlinks
			rep, err = benchrepo.Generate(ctx, *out, p, *seed)
		}
	default:
		_, _ = fmt.Fprintln(stderr, "genrepo: give --out <dir> to generate or --verify <repo> to inspect")
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
