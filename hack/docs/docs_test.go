//go:build docs

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package docs_test

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"sigs.k8s.io/yaml"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func TestSiteBuild(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/", "/scheduled-actions/"} {
		t.Run(prefix, func(t *testing.T) {
			destination := t.TempDir()
			cmd := exec.Command("hugo", "--source", "docs", "--destination", destination,
				"--baseURL", "https://docs.example.com"+prefix, "--minify", "--panicOnWarning")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "docs/node_modules/.bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Hugo build failed: %v\n%s", err, output)
			}
			type page struct {
				ids          map[string]bool
				descriptions map[string]string
				links        []string
				manifest     string
				parts        int
			}
			pages := map[string]page{}
			err := filepath.WalkDir(destination, func(file string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || filepath.Ext(file) != ".html" {
					return nil
				}
				data, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				doc, err := html.Parse(bytes.NewReader(data))
				if err != nil {
					return err
				}
				p := page{ids: map[string]bool{}, descriptions: map[string]string{}}
				var visit func(*html.Node)
				visit = func(node *html.Node) {
					if node.Type == html.ElementNode && node.Data == "meta" {
						var key, content string
						for _, attr := range node.Attr {
							switch attr.Key {
							case "name", "property":
								key = attr.Val
							case "content":
								content = attr.Val
							}
						}
						if key == "description" || key == "og:description" || key == "twitter:description" {
							p.descriptions[key] = content
						}
					}
					for _, attr := range node.Attr {
						switch attr.Key {
						case "data-window-part":
							p.parts++
						case "data-window-source":
							var content strings.Builder
							var collect func(*html.Node)
							collect = func(n *html.Node) {
								if n.Type == html.TextNode {
									content.WriteString(n.Data)
								}
								for child := n.FirstChild; child != nil; child = child.NextSibling {
									collect(child)
								}
							}
							collect(node)
							p.manifest = content.String()
						case "id":
							if p.ids[attr.Val] {
								t.Errorf("duplicate anchor %s in %s", attr.Val, file)
							}
							p.ids[attr.Val] = true
						case "src", "href":
							p.links = append(p.links, attr.Val)
						}
					}
					for child := node.FirstChild; child != nil; child = child.NextSibling {
						visit(child)
					}
				}
				visit(doc)
				rel, err := filepath.Rel(destination, file)
				if err != nil {
					return err
				}
				pages[filepath.ToSlash(rel)] = p
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(pages) < 16 {
				t.Fatalf("expected a multi-page site, got %d pages", len(pages))
			}
			// All three SEO surfaces must use explicit descriptions or the default,
			// without deriving summaries from rendered code blocks and shortcodes.
			for file, expected := range map[string]string{
				"docs/guides/actions/index.html":   "Change Windows documentation: schedule maintenance, protect planned work, and automate resource changes in Kubernetes.",
				"docs/guides/schedules/index.html": "Select namespaces and resources for ChangeWindows, understand protected namespaces, and configure schedules, time zones, and overlapping windows.",
				"docs/reference/api/index.html":    "Generated reference for the ChangeWindow Kubernetes API.",
			} {
				for _, key := range []string{"description", "og:description", "twitter:description"} {
					if got := pages[file].descriptions[key]; got != expected {
						t.Errorf("%s in %s: got %q, want %q", key, file, got, expected)
					}
				}
			}
			source, err := os.ReadFile(filepath.Join(root, "examples/quickstart.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if home := pages["index.html"]; home.parts != 7 || home.manifest != string(source) {
				t.Errorf("homepage must expose seven linked blocks and exact copyable YAML: parts=%d source matches=%t", home.parts, home.manifest == string(source))
			}
			checkLink := func(source, link string) {
				t.Helper()
				base, _ := url.Parse("https://docs.example.com" + prefix + strings.TrimSuffix(source, "index.html"))
				u, err := url.Parse(link)
				if err != nil {
					t.Errorf("invalid link %q: %v", link, err)
					return
				}
				u = base.ResolveReference(u)
				if u.Scheme != "https" || u.Host != base.Host {
					return
				}
				if !strings.HasPrefix(u.Path, prefix) {
					t.Errorf("link escapes hosting prefix: %s in %s", link, source)
					return
				}
				asset := strings.TrimPrefix(u.Path, prefix)
				if strings.HasSuffix(u.Path, "/") {
					asset += "index.html"
				}
				if _, err := os.Stat(filepath.Join(destination, filepath.FromSlash(asset))); err != nil {
					t.Errorf("missing local target %s in %s: %v", link, source, err)
				}
				if u.Fragment != "" && strings.HasSuffix(asset, ".html") && !pages[asset].ids[u.Fragment] {
					t.Errorf("broken fragment %s in %s", link, source)
				}
			}
			for file, p := range pages {
				for _, link := range p.links {
					checkLink(file, link)
				}
			}
			for file, anchors := range map[string][]string{
				"index.html": {"window-example-title", "window-help-admission", "window-help-config"},
				"docs/getting-started/installation/index.html": {"first-maintenance-window"},
				"docs/guides/actions/index.html":               {"downscale-and-restore", "rollout-restart"},
				"docs/guides/admission/index.html":             {"warning-periods", "audiences", "match-conditions"},
				"docs/examples/node-pool/index.html":           {"complete-example", "who-is-affected", "what-is-protected", "try-the-policy"},
				"docs/examples/conditional-changes/index.html": {"complete-example", "expected-behavior"},
				"docs/development/contributing/index.html":     {"tests-and-validation", "work-on-the-documentation"},
				"docs/reference/api/index.html":                {"changewindowspec", "changewindowstatus", "admissionwarning"},
			} {
				for _, anchor := range anchors {
					if !pages[file].ids[anchor] {
						t.Errorf("missing section %s in %s", anchor, file)
					}
				}
			}
			var search []struct{ Permalink, Title, Content string }
			data, err := os.ReadFile(filepath.Join(destination, "search-index.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &search); err != nil {
				t.Fatal(err)
			}
			if len(search) < 10 {
				t.Fatalf("missing guides from search: %d entries", len(search))
			}
			foundAPI := false
			for _, result := range search {
				checkLink("index.html", result.Permalink)
				if result.Title == "API reference" && strings.Contains(result.Content, "ChangeWindowSpec") {
					foundAPI = true
				}
			}
			if !foundAPI {
				t.Error("generated API types are not searchable")
			}
			for _, sample := range []string{"maintenance-only.yaml", "addons_v1alpha1_changewindow.yaml", "executor-rbac.yaml", "node-pool-maintenance.yaml", "conditional-maintenance.yaml", "quickstart.yaml", "freeze-except-pods.yaml", "after-hours.yaml", "after-hours-executor-rbac.yaml"} {
				original, err := os.ReadFile(filepath.Join(root, "examples", sample))
				if err != nil {
					t.Fatal(err)
				}
				published, err := os.ReadFile(filepath.Join(destination, "examples", sample))
				if err != nil || !bytes.Equal(original, published) {
					t.Errorf("download diverges from executable example %s: %v", sample, err)
				}
			}
		})
	}
}

func TestChangeWindowExamples(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples found: %v", err)
	}
	count := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var kind struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal(data, &kind); err != nil {
			t.Fatal(err)
		}
		if kind.Kind != "ChangeWindow" {
			continue
		}
		count++
		t.Run(filepath.Base(file), func(t *testing.T) {
			var action api.ChangeWindow
			if err := yaml.UnmarshalStrict(data, &action); err != nil {
				t.Fatal(err)
			}
			if _, err := policy.Compile(&action, testutil.Mapper()); err != nil {
				t.Fatalf("documented example fails policy validation: %v", err)
			}
		})
	}
	if count < 7 {
		t.Fatal("expected homepage, Node, conditional, admission-only and resource-action examples")
	}
}
