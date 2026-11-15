// Package churr holds the one test that reads the import graph of the whole
// module and fails when a rule of docs/STRUCTURE.md section 3 is broken, so
// the layering is enforced and not just written down.
package churr

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/tikaydaes/churr/"

// layers gives each package under internal/ its layer. A package may import
// packages in lower layers, and model. It may not import one in its own
// layer or above, except as mayAlso says.
var layers = map[string]int{
	"model": 0, "version": 0, "instant": 0,
	"config": 1, "secret": 1, "ratelimit": 1, "markdown": 1, "wirepb": 1, "memguard": 1,
	"store": 2,
	"blob":  3, "perm": 3,
	"notify": 4, "search": 4, "mod": 4, "preview": 4,
	"live": 5, "push": 5,
	"people": 6, "chat": 6,
	"forms": 7, "sched": 7, "bots": 7, "calls": 7, "sealed": 7, "link": 7, "auth": 7, "importer": 7, "export": 7, "janitor": 7, "acme": 7, "themes": 7,
	"api": 8, "web": 8,
	"server": 9, "hub": 9, "relay": 9,
}

// "themes" is the themes service; themes/palette is its pure colour
// arithmetic and imports nothing of ours.

// mayAlso lists the same-layer imports the document allows by name.
var mayAlso = map[string][]string{
	"push":  {"live"},
	"chat":  {"people"},
	"sched": {"forms"},
}

// onlyThese lists packages whose imports of ours are spelled out in full.
var onlyThese = map[string][]string{
	"perm":  {"model", "store"},
	"web":   {"version"},
	"hub":   {"link", "secret", "config", "ratelimit", "acme", "version", "model"},
	"relay": {"config", "ratelimit", "acme", "version"},
}

// owners lists the large dependencies that have one owner each.
var owners = []struct {
	prefix string
	may    []string
	why    string
}{
	{"modernc.org/sqlite", []string{"store/sqlite", "hub"}, "only store/sqlite (and the hub) import the SQLite driver"},
	{"github.com/jackc/pgx", []string{"store/postgres"}, "only store/postgres imports the PostgreSQL driver"},
	{"github.com/go-webauthn/", []string{"auth"}, "only auth imports the WebAuthn library"},
	{"github.com/pion/", []string{"calls"}, "only calls imports Pion"},
	{"github.com/minio/", []string{"blob"}, "only blob imports the object-store client"},
	{"google.golang.org/protobuf", []string{"wirepb", "link/wire"}, "only wirepb and the link's wire package speak Protocol Buffers"},
	{"github.com/coder/websocket", []string{"live", "link", "hub", "testkit"}, "only live, link and the hub speak WebSocket"},
	{"database/sql", []string{"store/sqlite", "store/postgres", "store/sqlstore", "store/migrations", "store/storetest", "hub"}, "no query is written outside the storage layer"},
}

func top(pkg string) string {
	if i := strings.Index(pkg, "/"); i >= 0 {
		return pkg[:i]
	}
	return pkg
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestImportRules(t *testing.T) {
	fset := token.NewFileSet()
	seen := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "testdata" || name == "docs" || name == "app" || name == "e2e" || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		isTest := strings.HasSuffix(path, "_test.go")
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		for _, imp := range f.Imports {
			target, _ := strconv.Unquote(imp.Path.Value)
			check(t, dir, path, isTest, target)
		}
		seen++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 50 {
		t.Fatalf("read only %d Go files; the walk has gone wrong", seen)
	}
}

func check(t *testing.T, dir, file string, isTest bool, target string) {
	pkg := strings.TrimPrefix(dir, "internal/")
	inInternal := strings.HasPrefix(dir, "internal/")

	// testkit is for tests only.
	if target == module+"internal/testkit" && !isTest {
		t.Errorf("%s imports testkit, which is for _test.go files only", file)
	}
	// The single-owner dependencies. Tests may use what they need to
	// stand a real thing up, and so may the test instruments in tools/.
	if !isTest && !strings.HasPrefix(dir, "tools/") {
		for _, o := range owners {
			if strings.HasPrefix(target, o.prefix) && !(inInternal && (contains(o.may, pkg) || contains(o.may, top(pkg)))) {
				t.Errorf("%s imports %s: %s", file, target, o.why)
			}
		}
	}
	if !strings.HasPrefix(target, module+"internal/") || isTest {
		return
	}
	to := strings.TrimPrefix(target, module+"internal/")

	if strings.HasPrefix(dir, "cmd/") {
		if !contains([]string{"config", "version", "server", "hub", "relay"}, top(to)) {
			t.Errorf("%s imports %s: a command imports config, version and server, hub or relay", file, to)
		}
		return
	}
	if !inInternal || top(pkg) == "testkit" {
		return
	}
	from, known := layers[top(pkg)]
	if !known {
		t.Errorf("%s: package %s has no layer in imports_test.go; give it the one docs/STRUCTURE.md gives it", file, pkg)
		return
	}
	if only, ok := onlyThese[top(pkg)]; ok && !contains(only, top(to)) {
		t.Errorf("%s imports %s: %s may import only %v of ours", file, to, pkg, only)
	}
	if top(to) == top(pkg) {
		return // a package and its own subpackages
	}
	toLayer, known := layers[top(to)]
	if !known {
		t.Errorf("%s imports %s, which has no layer in imports_test.go", file, to)
		return
	}
	if top(to) == "model" || toLayer < from || contains(mayAlso[top(pkg)], top(to)) {
		return
	}
	t.Errorf("%s (layer %d) imports %s (layer %d): a package imports only lower layers", file, from, to, toLayer)
}
