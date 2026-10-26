package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/gateway"
)

// openBotHome is a home with magpie's deepseek provider in it and no
// installed apps, and the path of OpenBot's magpie.json there.
func openBotHome(t *testing.T) (home, path string) {
	t.Helper()
	home, _ = museHome(t)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	was := appFolders
	apps := filepath.Join(home, "Applications")
	appFolders = func() []string { return []string{apps} }
	t.Cleanup(func() { appFolders = was })
	return home, filepath.Join(home, ".config", "openbot", "magpie.json")
}

func readOpenBot(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return m
}

// Connect, switch and disconnect on the file OpenBot makes at start ({}),
// and on one holding values of the user's own: magpie writes model,
// baseUrl and apiKey as OpenBot's reader takes them, and Disconnect puts
// back what each held, the file's other keys untouched.
func TestOpenBot(t *testing.T) {
	for _, tc := range []struct{ name, orig string }{
		{"as OpenBot makes it", "{}\n"},
		{"with the user's own", `{
  "model": "anthropic/claude-sonnet-5-5",
  "apiKey": "sk-own",
  "note": "kept"
}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, path := openBotHome(t)
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte(tc.orig), 0o644)
			var want map[string]any
			json.Unmarshal([]byte(tc.orig), &want)

			a := openBot(home)
			if a.Path != path || !a.Detected() {
				t.Fatalf("path %s, detected %v", a.Path, a.Detected())
			}
			if a.Check() != "" || a.Wired() {
				t.Fatalf("not connected yet: %q %v", a.Check(), a.Values())
			}
			f := a.Field("model")
			var vals []string
			for _, o := range f.Options(a.Values()) {
				vals = append(vals, o.Value)
			}
			if !slices.Contains(vals, "magpie/deepseek/pro") || !slices.Contains(vals, "magpie/deepseek/flash") {
				t.Fatalf("options: %v", vals)
			}

			// connect, then switch: one stash, the user's values kept in it
			for _, v := range []string{"magpie/deepseek/pro", "magpie/deepseek/pro", "magpie/deepseek/flash"} {
				if err := f.Set(v); err != nil {
					t.Fatal(err)
				}
				m := readOpenBot(t, path)
				ref := strings.TrimPrefix(v, "magpie/")
				if m["model"] != ref || m["baseUrl"] != gateway.URL() || m["apiKey"] != gateway.TokenFor("openbot") {
					t.Fatalf("after %s: %v", v, m)
				}
				if want["note"] != nil && m["note"] != want["note"] {
					t.Fatalf("the user's own key went: %v", m)
				}
				if f.Get() != v || !a.Wired() || a.Check() != "" {
					t.Fatalf("on %s: %q %v %q", v, f.Get(), a.Wired(), a.Check())
				}
			}
			if !strings.HasPrefix(gateway.TokenFor("openbot"), gateway.Token+"-") {
				t.Fatalf("key %q doesn't name openbot", gateway.TokenFor("openbot"))
			}

			// the gateway's address changed under it
			b, _ := os.ReadFile(path)
			os.WriteFile(path, []byte(strings.Replace(string(b), gateway.URL(), "http://127.0.0.1:1", 1)), 0o644)
			if c := a.Check(); !strings.Contains(c, "baseUrl") || !strings.Contains(c, "http://127.0.0.1:1") {
				t.Fatalf("check: %q", c)
			}
			os.WriteFile(path, b, 0o644)

			if err := a.Disconnect(); err != nil {
				t.Fatal(err)
			}
			if m := readOpenBot(t, path); !reflect.DeepEqual(m, want) {
				t.Fatalf("disconnect left %v, not %v", m, want)
			}
			if a.Wired() {
				t.Fatalf("still connected: %v", a.Values())
			}
		})
	}
}

// A magpie.json magpie made (OpenBot not started since it was installed)
// goes again on disconnect.
func TestOpenBotMadeFileGoes(t *testing.T) {
	home, path := openBotHome(t)
	a := openBot(home)
	f := a.Field("model")
	if err := f.Set("magpie/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if m := readOpenBot(t, path); m["model"] != "deepseek/flash" {
		t.Fatalf("new: %v", m)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		b, _ := os.ReadFile(path)
		t.Fatalf("made file left: %s", b)
	}
}

// OpenBot is found by the folder it makes at each start, or by its app
// installed and not opened yet: OpenBot.app on a Mac, OpenBot.exe in its
// folder under %LOCALAPPDATA%\Programs on Windows. Neither, and it isn't.
func TestOpenBotDetected(t *testing.T) {
	home, path := openBotHome(t)
	apps := appFolders()[0]
	if openBot(home).Detected() {
		t.Fatal("detected with nothing installed")
	}
	// a folder skill installers make for every agent is no sign
	os.MkdirAll(filepath.Join(filepath.Dir(path), "skills"), 0o755)
	if openBot(home).Detected() {
		t.Fatal("detected by a skills folder alone")
	}
	os.RemoveAll(filepath.Dir(path))

	os.MkdirAll(filepath.Join(apps, "openbot"), 0o755)
	os.WriteFile(filepath.Join(apps, "openbot", "OpenBot.exe"), []byte("MZ"), 0o755)
	if !openBot(home).Detected() {
		t.Fatal("OpenBot.exe not found")
	}
	os.RemoveAll(filepath.Join(apps, "openbot"))

	os.MkdirAll(filepath.Join(apps, "OpenBot.app", "Contents"), 0o755)
	if !openBot(home).Detected() {
		t.Fatal("OpenBot.app not found")
	}
	os.RemoveAll(filepath.Join(apps, "OpenBot.app"))

	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{}\n"), 0o644)
	if !openBot(home).Detected() {
		t.Fatal("OpenBot's folder not found")
	}
}

// OpenBot is listed, its calls known by its key and User-Agent, and its
// advice is one of the notices the GUI translates.
func TestOpenBotListed(t *testing.T) {
	openBotHome(t)
	a, err := Find("openbot")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "OpenBot" || a.Icon != "openbot-color" || !slices.Contains(a.UA, "openbot") {
		t.Fatalf("%+v", a)
	}
	if _, err := os.Stat(filepath.Join("..", "gui", "assets", "icons", a.Icon+".svg")); err != nil {
		t.Fatal(err)
	}
	if n := a.Notice(); n == "" || !slices.Contains(Notices(), n) {
		t.Fatalf("notice %q", n)
	}
}
