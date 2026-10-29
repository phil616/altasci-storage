package ascli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type downloader struct {
	options              options
	client               *http.Client
	log                  *slog.Logger
	version, base, grant string
	root                 *os.Root
	plan                 []entry
	seen                 map[string]bool
	paths                map[string]bool
}

type node struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"node_type"`
	Size int64  `json:"size"`
}
type entry struct {
	node
	path string
}

// Run writes all output to out and returns a stable process exit code. It never
// prompts, so a missing password cannot hang a non-interactive CI job.
func Run(ctx context.Context, args []string, out io.Writer, getenv func(string) string, version string) int {
	o, err := parseOptions(args, out, getenv)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var handler slog.Handler = slog.NewTextHandler(out, nil)
	if o.logFormat == "json" {
		handler = slog.NewJSONHandler(out, nil)
	}
	log := slog.New(handler)
	if err != nil {
		log.Error(err.Error(), "exit_code", 2)
		return 2
	}
	if o.version {
		fmt.Fprintln(out, "ascli "+version)
		return 0
	}
	client, transport := newClient(o)
	defer transport.CloseIdleConnections()
	d := &downloader{options: o, client: client, log: log, version: version, seen: map[string]bool{}, paths: map[string]bool{}}
	err = d.run(ctx)
	if ctx.Err() != nil {
		log.Error("download interrupted", "exit_code", 130)
		return 130
	}
	if err != nil {
		code := 1
		var f *failure
		if errors.As(err, &f) {
			code = f.code
		}
		log.Error(err.Error(), "exit_code", code)
		return code
	}
	log.Info("all downloads completed")
	return 0
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
var deviceNamePattern = regexp.MustCompile(`^(COM|LPT)[1-9¹²³]$`)

func (d *downloader) run(ctx context.Context) error {
	u, err := parseURL(d.options.link)
	if err != nil {
		return err
	}
	direct := presigned(u)
	if !direct {
		if err := d.prepareShare(ctx, u); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(d.options.directory, 0755); err != nil {
		return fail(1, "cannot create download directory")
	}
	d.root, err = os.OpenRoot(d.options.directory)
	if err != nil {
		return fail(1, "cannot open download directory")
	}
	defer d.root.Close()
	if direct {
		d.log.Info("downloading directly from object storage")
		return d.download(ctx, u.String(), "", "", -1)
	}
	// Check the whole manifest before transferring bytes so name collisions and
	// existing files cannot silently produce a partial or ambiguous result.
	for _, item := range d.plan {
		info, err := d.root.Lstat(item.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fail(1, "cannot inspect destination path")
		}
		if item.Type == "directory" && info.IsDir() {
			continue
		}
		if item.Type == "file" && info.Mode().IsRegular() && d.options.overwrite {
			continue
		}
		return fail(1, fmt.Sprintf("destination exists or is unsafe: %q (use --overwrite for regular files)", item.path))
	}
	d.log.Info("share verified", "entries", len(d.plan))
	for _, item := range d.plan {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if item.Type == "directory" {
			if err := d.root.MkdirAll(item.path, 0755); err != nil {
				return fail(1, "cannot create shared directory")
			}
			continue
		}
		var target struct {
			Delivery string `json:"delivery"`
			URL      string `json:"url"`
		}
		if err := d.json(ctx, http.MethodPost, d.base+"/nodes/"+url.PathEscape(item.ID)+"/download", d.grant, strings.NewReader("{}"), &target); err != nil {
			return err
		}
		grant := ""
		switch target.Delivery {
		case "local":
			// The grant is only valid for the exact API content endpoint. Never
			// forward it to a provider or an arbitrary server-supplied URL.
			if target.URL != d.base+"/nodes/"+url.PathEscape(item.ID)+"/content" {
				return fail(1, "unexpected local content URL")
			}
			grant = d.grant
		case "external":
			// No grant, cookies, API keys or password go to object storage.
		default:
			return fail(1, "unknown download delivery type")
		}
		if err := d.download(ctx, target.URL, grant, item.path, item.Size); err != nil {
			return err
		}
	}
	return nil
}

func (d *downloader) prepareShare(ctx context.Context, u *url.URL) error {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var token string
	switch {
	case len(parts) == 2 && parts[0] == "s":
		token = parts[1]
	case len(parts) == 5 && strings.Join(parts[:4], "/") == "api/v1/public/shares":
		token = parts[4]
	default:
		return fail(2, "expected an AltasCI /s/<token> share link or an S3/OSS presigned URL")
	}
	if !tokenPattern.MatchString(token) {
		return fail(2, "invalid share token")
	}
	apiURL := d.options.apiURL
	if apiURL == "" && len(parts) == 5 {
		apiURL = u.Scheme + "://" + u.Host
	}
	if apiURL == "" {
		var discovery struct {
			APIURL string `json:"api_url"`
		}
		if err := d.json(ctx, http.MethodGet, u.Scheme+"://"+u.Host+"/.well-known/altasci.json", "", nil, &discovery); err != nil {
			return fail(2, "cannot discover API origin; use --api-url or ASCLI_API_URL for this deployment")
		}
		apiURL = discovery.APIURL
	}
	apiURL, err := origin(apiURL)
	if err != nil {
		return err
	}
	d.base = apiURL + "/api/v1/public/shares/" + token
	var meta struct {
		Target        node `json:"target"`
		GrantRequired bool `json:"grant_required"`
	}
	if err := d.json(ctx, http.MethodGet, d.base+"/", "", nil, &meta); err != nil {
		return err
	}
	if meta.GrantRequired {
		password := d.options.password
		if password == "" {
			password = u.Query().Get("code")
		}
		if password == "" {
			return fail(2, "share password required; use --pwd or ASCLI_PWD")
		}
		body, _ := json.Marshal(map[string]string{"code": password})
		var verified struct {
			Grant string `json:"grant"`
		}
		if err := d.json(ctx, http.MethodPost, d.base+"/verify", "", strings.NewReader(string(body)), &verified); err != nil {
			return err
		}
		if verified.Grant == "" {
			return fail(1, "verification response is missing a grant")
		}
		d.grant = verified.Grant
	}
	if meta.Target.ID != "" {
		return d.walk(ctx, meta.Target, "", 0)
	}
	// Multiple targets use a synthetic directory; list its selected roots.
	if meta.Target.Type != "directory" {
		return fail(1, "invalid share metadata")
	}
	roots, err := d.list(ctx, "")
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return fail(3, "share contains no available targets")
	}
	for _, n := range roots {
		if err := d.walk(ctx, n, "", 0); err != nil {
			return err
		}
	}
	return nil
}

func (d *downloader) list(ctx context.Context, parent string) ([]node, error) {
	raw := d.base + "/nodes"
	if parent != "" {
		raw += "?parent_id=" + url.QueryEscape(parent)
	}
	var result struct {
		Items []node `json:"items"`
	}
	err := d.json(ctx, http.MethodGet, raw, d.grant, nil, &result)
	return result.Items, err
}

func (d *downloader) walk(ctx context.Context, n node, parent string, depth int) error {
	if depth > 128 {
		return fail(1, "shared directory nesting exceeds 128 levels")
	}
	if !tokenPattern.MatchString(n.ID) || !safeName(n.Name) || n.Size < 0 || (n.Type != "file" && n.Type != "directory") {
		return fail(1, "share contains an invalid node or unsafe filename")
	}
	if d.seen[n.ID] {
		return nil
	} // overlapping selections are downloaded once
	d.seen[n.ID] = true
	name := filepath.Join(parent, n.Name)
	key := strings.ToLower(name)
	if d.paths[key] {
		return fail(1, fmt.Sprintf("shared names collide at %q", name))
	}
	d.paths[key] = true
	d.plan = append(d.plan, entry{node: n, path: name})
	if n.Type == "file" {
		return nil
	}
	children, err := d.list(ctx, n.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := d.walk(ctx, child, name, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// Validate one portable path component. Root confines filesystem operations,
// including existing symlinks, to the selected directory tree.
func safeName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:*?"<>|`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || !filepath.IsLocal(name) {
		return false
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return false
		}
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if deviceNamePattern.MatchString(stem) {
		return false
	}
	return true
}

func (d *downloader) download(ctx context.Context, raw, grant, name string, expected int64) error {
	resp, err := d.request(ctx, http.MethodGet, raw, grant, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp, grant != "")
	}
	if name == "" {
		_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
		name = params["filename"]
		if name == "" {
			u, _ := url.Parse(raw)
			_, params, _ = mime.ParseMediaType(u.Query().Get("response-content-disposition"))
			name = params["filename"]
			if name == "" {
				name = path.Base(u.Path)
			}
		}
		if !safeName(name) {
			return fail(1, "presigned URL returned an unsafe or missing filename")
		}
	}
	if expected >= 0 && resp.ContentLength >= 0 && resp.ContentLength != expected {
		return fail(1, "download size differs from share metadata")
	}
	if expected < 0 {
		expected = resp.ContentLength
	}
	if err := d.checkFile(name); err != nil {
		return err
	}
	if err := d.root.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return fail(1, "cannot create destination directory")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fail(1, "cannot allocate temporary filename")
	}
	temp := filepath.Join(filepath.Dir(name), ".ascli-"+hex.EncodeToString(random[:])+".part")
	f, err := d.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail(1, "cannot create temporary download file")
	}
	defer d.root.Remove(temp)
	defer f.Close()
	d.log.Info("downloading", "file", name)
	reader := io.Reader(resp.Body)
	if expected >= 0 && expected < 1<<63-1 {
		reader = io.LimitReader(reader, expected+1)
	}
	n, err := io.Copy(f, reader)
	if err != nil {
		return fail(1, "download stream or disk write failed")
	}
	if expected >= 0 && n != expected {
		return fail(1, "download incomplete or size mismatch")
	}
	if err := f.Sync(); err != nil {
		return fail(1, "cannot sync downloaded file")
	}
	if err := f.Close(); err != nil {
		return fail(1, "cannot close downloaded file")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := d.checkFile(name); err != nil {
		return err
	}
	if d.options.overwrite {
		err = d.root.Rename(temp, name)
	} else {
		// Hard linking publishes a complete file atomically and fails if a
		// competing process created the destination after the preflight check.
		err = d.root.Link(temp, name)
	}
	if err != nil {
		return fail(1, "cannot publish download (destination conflict or filesystem error)")
	}
	d.log.Info("download completed", "file", name, "bytes", n)
	return nil
}

func (d *downloader) checkFile(name string) error {
	info, err := d.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fail(1, "cannot inspect destination file")
	}
	if !info.Mode().IsRegular() || !d.options.overwrite {
		return fail(1, fmt.Sprintf("destination exists or is unsafe: %q (use --overwrite for regular files)", name))
	}
	return nil
}
