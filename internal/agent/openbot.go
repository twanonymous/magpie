package agent

// OpenBot (nightly-labs/openbot, a desktop app that runs coding agents)
// reads what magpie sets for it from a file of its own,
// ~/.config/openbot/magpie.json, the same path under the user's folder on
// Windows (it doesn't read XDG_CONFIG_HOME). OpenBot makes the folder, and
// the file as {}, at each start, and watches it (src/backend/magpie-link.ts,
// src/main/magpie-link-watcher.ts):
//
//	{"model": "deepseek/pro", "baseUrl": "http://127.0.0.1:3425", "apiKey": "magpie-openbot"}
//
// model is the catalog id new Magpie agents in OpenBot start on, sent as it
// is (no "magpie/"); baseUrl is the gateway's root, to which OpenBot adds
// the paths and which it takes only on loopback; apiKey is the key that
// names its calls OpenBot's. A value missing or failing OpenBot's checks
// counts as absent, and with baseUrl and apiKey absent OpenBot finds the
// gateway itself. OpenBot rereads the file within seconds, so nothing needs
// restarting.
//
// magpie writes the three keys and stashes what each held, put back when
// magpie steps out; the file's other keys are left as they are, and a file
// magpie made goes again once it is empty.

import (
	"path/filepath"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

const (
	openBotModel = "model"
	openBotURL   = "baseUrl"
	openBotKey   = "apiKey"
)

// openBotApps are what OpenBot's installers leave: OpenBot.app on a Mac,
// and on Windows OpenBot.exe in its folder under %LOCALAPPDATA%\Programs
// (electron-builder's per-user NSIS, productName OpenBot).
var openBotApps = []string{"OpenBot.app", "OpenBot.exe"}

func openBot(home string) *Agent {
	at := here(home)
	dir := filepath.Join(home, ".config", "openbot")
	path := filepath.Join(dir, "magpie.json")
	stashKey := func(k string) string { return "openbot:" + path + ":" + k }
	keyNew := stashKey("new")
	keys := []string{openBotModel, openBotURL, openBotKey}
	get := func(k string) string { v, _ := edit.GetJSON(path, k); return v }
	put := func(k, v string) error {
		if v == "" {
			if !isFile(path) {
				return nil
			}
			return edit.DelJSON(path, k)
		}
		return edit.SetJSON(path, edit.KV{Path: k, Value: v})
	}
	// ours: the key in the file is one magpie gives OpenBot
	ours := func() bool {
		k := get(openBotKey)
		return k != "" && (k == agentKeyAt("openbot", at.gw()) || k == gateway.TokenFor("openbot") || ourKey(k))
	}
	// on: OpenBot starts on one of magpie's models through the gateway
	on := func() bool { return ours() && isMagpie(get(openBotModel)) }
	return atomic(&Agent{
		ID: "openbot", Name: "OpenBot", Icon: "openbot-color", Spelled: prefixed, NoNames: true,
		UA:  []string{"openbot"},
		Dir: dir, Path: path,
		// the folder OpenBot makes at each start, or the app installed and
		// not opened yet
		detect: func() bool {
			if agentDir(dir) {
				return true
			}
			for _, p := range inAppFolders(appFolders(), openBotApps...) {
				if strings.HasSuffix(p, ".app") && isDir(p) || strings.HasSuffix(p, ".exe") && isFile(p) {
					return true
				}
			}
			return false
		},
		Notice: func() string { return noticeOpenBot.String() },
		Check: func() string {
			if !on() {
				return ""
			}
			return wiringOff("OpenBot", path, func(k string) (string, bool) { return edit.GetJSON(path, k) },
				openBotURL, at.gw())
		},
		Fields: []Field{{
			Key: "model", Label: "model",
			Get: func() string {
				m := get(openBotModel)
				if m != "" && ours() {
					return magpieID + "/" + m
				}
				return m
			},
			Set: func(v string) error {
				if ref, ok := strings.CutPrefix(v, magpieID+"/"); ok && isMagpie(ref) {
					// what OpenBot had, unless magpie stashed it already
					if !ours() {
						had := map[string]string{}
						for _, k := range keys {
							forget(stashKey(k))
							had[stashKey(k)] = get(k)
						}
						forget(keyNew)
						stash(had)
						if !isFile(path) {
							stash(map[string]string{keyNew: "1"})
						}
					}
					return edit.SetJSON(path,
						edit.KV{Path: openBotModel, Value: ref},
						edit.KV{Path: openBotURL, Value: at.gw()},
						edit.KV{Path: openBotKey, Value: agentKeyAt("openbot", at.gw())})
				}
				made := false
				if ours() {
					made = unstash(keyNew) != ""
					// back to what OpenBot had
					for _, k := range []string{openBotURL, openBotKey} {
						if err := put(k, unstash(stashKey(k))); err != nil {
							return err
						}
					}
					if v == "" {
						v = unstash(stashKey(openBotModel))
					} else {
						forget(stashKey(openBotModel))
					}
				}
				if err := put(openBotModel, v); err != nil {
					return err
				}
				if made {
					if raw, err := edit.Read(path); err == nil && len(gjson.ParseBytes(raw).Map()) == 0 {
						return edit.Remove(path)
					}
				}
				return nil
			},
			Options: func(cur map[string]string) []Option {
				var own []Option
				if c := cur["model"]; c != "" && !usesMagpie(c) {
					_, id, _ := strings.Cut(c, "/")
					own = append(own, Option{Value: c, Icon: modelIcon("", id)})
				}
				return append(group("OpenBot", own), viaMagpie("openbot", magpieID+"/")...)
			},
		}},
	}, path)
}

// what OpenBot says after a change (notice.go)
var (
	noticeOpenBot = newNotice("OpenBot reads this within a few seconds — new Magpie agents in OpenBot start on this model; open ones keep the model they have.")
)
