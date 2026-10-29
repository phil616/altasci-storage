package ascli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func execute(ctx context.Context, args ...string) (int, string) {
	var out bytes.Buffer
	code := Run(ctx, args, &out, func(string) string { return "" }, "test")
	return code, out.String()
}

func sendJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func assertFile(t *testing.T, name, want string) {
	t.Helper()
	got, err := os.ReadFile(name)
	if err != nil || string(got) != want {
		t.Fatalf("file %s: got %q, error %v; want %q", name, got, err, want)
	}
}

func TestShareDiscoveryPasswordTreeAndExternalDelivery(t *testing.T) {
	var objectHits, verifyHits, contentHits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		objectHits.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.URL.RawQuery != "X-Amz-Signature=secret%2Bsig&opaque=a+b%2Fc" {
			t.Error("credentials leaked or signed query changed")
		}
		fmt.Fprint(w, "hello")
	}))
	defer provider.Close()
	var apiURL string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/api/v1/public/shares/01234-56789"
		switch r.URL.Path {
		case base + "/":
			sendJSON(w, map[string]any{"grant_required": true, "target": node{Type: "directory"}})
		case base + "/verify":
			verifyHits.Add(1)
			var v map[string]string
			_ = json.NewDecoder(r.Body).Decode(&v)
			if r.Method != "POST" || v["code"] != "0123" {
				t.Error("incorrect password/method")
			}
			sendJSON(w, map[string]string{"grant": "sensitive-grant"})
		default:
			if r.Header.Get("Authorization") != "Bearer sensitive-grant" {
				t.Error("missing grant")
			}
			switch r.URL.Path {
			case base + "/nodes":
				var items []node
				switch r.URL.Query().Get("parent_id") {
				case "":
					items = []node{{ID: "dir", Name: "目录", Type: "directory"}, {ID: "local", Name: "local.txt", Type: "file", Size: 5}}
				case "dir":
					items = []node{{ID: "remote", Name: "文件.txt", Type: "file", Size: 5}, {ID: "empty", Name: "empty", Type: "directory"}}
				case "empty":
					items = []node{}
				default:
					t.Error("unexpected parent")
				}
				sendJSON(w, map[string]any{"items": items})
			case base + "/nodes/remote/download":
				if r.Method != "POST" {
					t.Error("expected POST download")
				}
				sendJSON(w, map[string]string{"delivery": "external", "url": provider.URL + "/object?X-Amz-Signature=secret%2Bsig&opaque=a+b%2Fc"})
			case base + "/nodes/local/download":
				sendJSON(w, map[string]string{"delivery": "local", "url": apiURL + base + "/nodes/local/content"})
			case base + "/nodes/local/content":
				contentHits.Add(1)
				fmt.Fprint(w, "local")
			default:
				t.Errorf("unexpected API request: %s", r.URL.Path)
				w.WriteHeader(500)
			}
		}
	}))
	defer api.Close()
	apiURL = api.URL
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/altasci.json" || r.URL.RawQuery != "" {
			t.Error("discovery leaked share information")
		}
		sendJSON(w, map[string]string{"api_url": api.URL})
	}))
	defer web.Close()
	dir := filepath.Join(t.TempDir(), "new", "downloads")
	code, logs := execute(context.Background(), web.URL+"/s/01234-56789?code=wrong", "--pwd", "0123", "--download-dir", dir, "--log-format", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, logs)
	}
	assertFile(t, filepath.Join(dir, "目录", "文件.txt"), "hello")
	assertFile(t, filepath.Join(dir, "local.txt"), "local")
	if info, err := os.Stat(filepath.Join(dir, "目录", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("missing empty directory: %v", err)
	}
	if objectHits.Load() != 1 || contentHits.Load() != 1 || verifyHits.Load() != 1 {
		t.Fatal("unexpected traffic counts")
	}
	for _, secret := range []string{"0123", "sensitive-grant", "secret%2Bsig", "opaque=", "01234-56789"} {
		if strings.Contains(logs, secret) {
			t.Errorf("secret in logs: %s", secret)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if !json.Valid([]byte(line)) {
			t.Error("invalid JSON log")
		}
	}
}

func TestDirectPresignedDownloadAndDefaultDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.URL.Query().Get("X-Amz-Signature") != "secret" {
			t.Error("signed URL or headers changed")
		}
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.txt")
		fmt.Fprint(w, "bytes")
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Chdir(dir)
	code, logs := execute(context.Background(), server.URL+"/s/object?X-Amz-Signature=secret", "--pwd", "ignored")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, logs)
	}
	assertFile(t, filepath.Join(dir, "报告.txt"), "bytes")
	// Do not overwrite an existing destination, even for raw URLs.
	code, _ = execute(context.Background(), server.URL+"/obj?X-Amz-Signature=secret")
	if code != 1 {
		t.Fatalf("existing file exit: %d", code)
	}
	code, logs = execute(context.Background(), "--overwrite", server.URL+"/obj?X-Amz-Signature=secret")
	if code != 0 {
		t.Fatalf("overwrite exit %d: %s", code, logs)
	}
}

func TestTransferFailuresNeverPublishPartialFile(t *testing.T) {
	for _, mode := range []string{"truncated", "idle", "timeout", "forbidden", "unsafe-name", "partial-status"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "forbidden":
					w.WriteHeader(403)
					fmt.Fprint(w, "secret provider error")
					return
				case "unsafe-name":
					w.Header().Set("Content-Disposition", `attachment; filename="../escape.txt"`)
				case "partial-status":
					w.WriteHeader(206)
				case "truncated":
					w.Header().Set("Content-Length", "100")
				case "idle", "timeout":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				fmt.Fprint(w, "short")
			}))
			defer server.Close()
			dir := t.TempDir()
			args := []string{server.URL + "/file?X-Amz-Signature=secret", "--download-dir", dir}
			if mode == "idle" {
				args = append(args, "--idle-timeout", "50ms")
			}
			if mode == "timeout" {
				args = append(args, "--timeout", "50ms")
			}
			code, logs := execute(context.Background(), args...)
			want := 1
			if mode == "forbidden" {
				want = 3
			}
			if code != want {
				t.Fatalf("exit %d, want %d: %s", code, want, logs)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("partial/temporary file retained")
			}
			if strings.Contains(logs, "secret") {
				t.Fatal("secret logged")
			}
		})
	}
}

func TestShareFailuresAndNoPasswordRetries(t *testing.T) {
	for _, status := range []int{401, 403, 404, 410, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var verifies atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/verify") {
					verifies.Add(1)
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(status)
					sendJSON(w, map[string]any{"error": map[string]string{"code": "SHARE_CODE_INVALID", "message": "secret-password"}})
					return
				}
				sendJSON(w, map[string]any{"grant_required": true, "target": node{ID: "file", Name: "f.txt", Type: "file"}})
			}))
			defer server.Close()
			code, logs := execute(context.Background(), server.URL+"/api/v1/public/shares/token/", "--pwd", "secret-password", "--download-dir", t.TempDir())
			want := 3
			if status == 500 {
				want = 1
			}
			if code != want || verifies.Load() != 1 || strings.Contains(logs, "secret-password") {
				t.Fatalf("exit=%d verifies=%d logs=%s", code, verifies.Load(), logs)
			}
			code, _ = execute(context.Background(), server.URL+"/api/v1/public/shares/token/")
			if code != 2 || verifies.Load() != 1 {
				t.Fatal("missing password must fail without prompting/verification")
			}
		})
	}
}

func TestManifestRejectsUnsafeAndCollidingNames(t *testing.T) {
	for _, names := range [][]string{{"../escape"}, {`..\escape`}, {"NUL"}, {"COM¹.txt"}, {"f.txt", "F.txt"}, {"same", "same"}} {
		t.Run(strings.Join(names, "-"), func(t *testing.T) {
			var downloads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/nodes") {
					var items []node
					for i, name := range names {
						items = append(items, node{ID: fmt.Sprint(i), Name: name, Type: "file"})
					}
					sendJSON(w, map[string]any{"items": items})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/download") {
					downloads.Add(1)
					w.WriteHeader(500)
					return
				}
				sendJSON(w, map[string]any{"target": node{Type: "directory"}})
			}))
			defer server.Close()
			code, logs := execute(context.Background(), server.URL+"/api/v1/public/shares/token/", "--download-dir", t.TempDir())
			if code != 1 || downloads.Load() != 0 {
				t.Fatalf("exit %d: %s", code, logs)
			}
		})
	}
}

func TestFilesystemConfinementAndPreserveExistingOnFailure(t *testing.T) {
	outside := t.TempDir()
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	d := &downloader{options: options{}, root: nil}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	d.root = root
	if err := d.checkFile("linked/out.txt"); err == nil {
		t.Fatal("outside symlink traversal allowed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "bad")
	}))
	defer server.Close()
	requireWrite := os.WriteFile(filepath.Join(dir, "file"), []byte("original"), 0600)
	if requireWrite != nil {
		t.Fatal(requireWrite)
	}
	code, _ := execute(context.Background(), server.URL+"/file?X-Amz-Signature=secret", "--download-dir", dir, "--overwrite")
	if code != 1 {
		t.Fatal("incomplete overwrite should fail")
	}
	assertFile(t, filepath.Join(dir, "file"), "original")
}

func TestNeverForwardGrantToRedirectOrExternalLocalURL(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); fmt.Fprint(w, "secret") }))
	defer other.Close()
	for _, redirect := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/verify"):
				sendJSON(w, map[string]string{"grant": "grant"})
			case strings.HasSuffix(r.URL.Path, "/download"):
				if redirect {
					http.Redirect(w, r, other.URL, 302)
					return
				}
				sendJSON(w, map[string]string{"delivery": "local", "url": other.URL})
			default:
				sendJSON(w, map[string]any{"grant_required": true, "target": node{ID: "file", Name: "f", Type: "file"}})
			}
		}))
		code, _ := execute(context.Background(), server.URL+"/api/v1/public/shares/token/", "--pwd", "1234", "--download-dir", t.TempDir())
		server.Close()
		if code != 1 || hits.Load() != 0 {
			t.Fatal("grant may have leaked to another origin")
		}
	}
}

func TestVersionOutput(t *testing.T) {
	for _, version := range []string{"dev", "v1.2.3", "v2.0.0-rc.1"} {
		t.Run(version, func(t *testing.T) {
			var out bytes.Buffer
			code := Run(context.Background(), []string{"--version"}, &out, func(string) string { return "" }, version)
			if code != 0 || out.String() != "ascli "+version+"\n" {
				t.Fatalf("exit=%d output=%q", code, out.String())
			}
		})
	}
}

func TestConfigurationEnvironmentCancellationAndHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--unknown"}, {"https://web.test/s/token", "--timeout", "0s"}, {"https://web.test/s/token", "--pwd"}, {"http://remote.test/s/token"}, {"ftp://remote.test/file"}, {"https://user:pass@web.test/s/token"}, {"https://web.test/s/token", "second"}} {
		code, _ := execute(context.Background(), args...)
		if code != 2 {
			t.Errorf("args %v: exit %d", args, code)
		}
	}
	o, err := parseOptions([]string{"https://web.test/s/token", "--download-dir", "out", "--pwd=override"}, io.Discard, func(key string) string {
		return map[string]string{"ASCLI_PWD": "env", "ASCLI_API_URL": "https://api.test"}[key]
	})
	if err != nil || o.password != "override" || o.apiURL != "https://api.test" || o.directory != "out" {
		t.Fatalf("options: %+v %v", o, err)
	}
	for _, arg := range []string{"--help", "--version"} {
		code, logs := execute(context.Background(), arg)
		if code != 0 || logs == "" {
			t.Fatal("help/version failed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _ := execute(ctx, "http://127.0.0.1:1/api/v1/public/shares/token/")
	if code != 130 {
		t.Fatalf("interrupt exit %d", code)
	}
	start := time.Now()
	code, logs := execute(context.Background(), "http://127.0.0.1:1/s/token?code=sensitive", "--timeout", "100ms")
	if code != 2 || time.Since(start) > time.Second || strings.Contains(logs, "sensitive") {
		t.Fatalf("discovery error exit %d: %s", code, logs)
	}
}
