// Package ascli implements the non-interactive public-share downloader.
package ascli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

type options struct {
	link, password, apiURL, directory, logFormat string
	timeout, idleTimeout                         time.Duration
	overwrite, version                           bool
}

const usage = `Usage: ascli <share-link-or-presigned-url> [options]

Options may appear before or after the URL. Quote URLs containing '&'.
  --pwd CODE             Share password (default: ASCLI_PWD, then link ?code=)
  --download-dir DIR     Download directory (default: current directory)
  --api-url ORIGIN       API origin for older Web deployments (ASCLI_API_URL)
  --overwrite            Replace existing regular files after successful download
  --timeout DURATION     Total timeout per HTTP request (default: 30m)
  --idle-timeout DURATION  Response header/body idle timeout (default: 30s)
  --log-format FORMAT    text or json; all logs go to STDOUT
  --version              Print version
  --help                 Print help

Exit codes: 0 success; 1 transfer/filesystem/protocol error; 2 invalid arguments
or API discovery configuration; 3 access denied/expired/unavailable share/rate limit;
130 interrupted. Stops on the first error; never prompts or retries passwords.
`

func parseOptions(args []string, out io.Writer, getenv func(string) string) (options, error) {
	o := options{}
	f := flag.NewFlagSet("ascli", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.password, "pwd", getenv("ASCLI_PWD"), "")
	f.StringVar(&o.apiURL, "api-url", getenv("ASCLI_API_URL"), "")
	f.StringVar(&o.directory, "download-dir", ".", "")
	f.StringVar(&o.logFormat, "log-format", "text", "")
	f.DurationVar(&o.timeout, "timeout", 30*time.Minute, "")
	f.DurationVar(&o.idleTimeout, "idle-timeout", 30*time.Second, "")
	f.BoolVar(&o.overwrite, "overwrite", false, "")
	f.BoolVar(&o.version, "version", false, "")
	// flag stops at the first positional argument. Partition first so the
	// documented `ascli URL --pwd CODE` syntax works without dependencies.
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		if name == "h" || name == "help" {
			fmt.Fprint(out, usage)
			return o, flag.ErrHelp
		}
		entry := f.Lookup(name)
		if entry == nil {
			return o, errors.New("unknown option; see --help")
		}
		flags = append(flags, arg)
		isBool := name == "overwrite" || name == "version"
		if !isBool && !strings.Contains(arg, "=") {
			i++
			if i >= len(args) {
				return o, errors.New("option needs a value; see --help")
			}
			flags = append(flags, args[i])
		}
	}
	if err := f.Parse(flags); err != nil {
		return o, errors.New("invalid option value; see --help")
	}
	if o.version {
		return o, nil
	}
	if len(positional) != 1 {
		return o, errors.New("provide exactly one share link or presigned URL; see --help")
	}
	o.link = positional[0]
	if o.logFormat != "text" && o.logFormat != "json" {
		return o, errors.New("--log-format must be text or json")
	}
	if o.timeout <= 0 || o.idleTimeout <= 0 || o.directory == "" {
		return o, errors.New("timeouts must be positive and --download-dir must not be empty")
	}
	return o, nil
}
