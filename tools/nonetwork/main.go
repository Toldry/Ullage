// Command nonetwork is a build check for safety rule 1 (no outbound network). It fails
// when shipped Go code references an API that can open an outbound connection, when
// go.mod gains an unreviewed module, or when frontend files reference a remote URL.
// It runs in CI and as a test (`go test ./tools/nonetwork`).
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// forbiddenSymbols maps an import path to symbols that can reach the network. A nil
// slice forbids importing the package at all.
var forbiddenSymbols = map[string][]string{
	"net": {"Dial", "DialTimeout", "DialTCP", "DialUDP", "DialIP", "DialUnix", "Dialer",
		"LookupHost", "LookupIP", "LookupAddr", "LookupCNAME", "LookupMX", "LookupTXT",
		"LookupSRV", "LookupNS", "LookupPort", "ResolveIPAddr", "ResolveUDPAddr"},
	"net/http": {"Get", "Post", "PostForm", "Head", "Client", "NewRequest",
		"NewRequestWithContext", "Transport", "DefaultClient", "DefaultTransport",
		"ProxyFromEnvironment"},
	"crypto/tls":         {"Dial", "DialWithDialer", "Dialer"},
	"os/exec":            {"Command", "CommandContext"},
	"net/smtp":           nil,
	"net/rpc":            nil,
	"net/rpc/jsonrpc":    nil,
	"net/http/httputil":  nil,
	"net/mail":           nil,
	"net/http/cookiejar": nil,
}

// allowedUses lists reviewed exceptions as "file:importPath.Symbol".
var allowedUses = map[string]string{
	// Replaces the defaults with refusing implementations.
	"internal/nonetwork/nonetwork.go:net/http.DefaultTransport": "installs blocker",
	"internal/nonetwork/nonetwork.go:net/http.DefaultClient":    "installs blocker",
	"internal/nonetwork/nonetwork.go:net/http.Client":           "installs blocker",
	// xdg-open with the local launch URL; Windows uses ShellExecute instead.
	"internal/platform/platform_linux.go:os/exec.Command": "opens local URL in browser",
}

// allowedModules are the only third-party modules a release may depend on.
var allowedModules = map[string]bool{
	"github.com/BurntSushi/toml": true,
	"golang.org/x/sys":           true,
}

type finding struct {
	location string
	message  string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	findings, err := check(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nonetwork:", err)
		os.Exit(2)
	}
	for _, found := range findings {
		fmt.Fprintf(os.Stderr, "%s: %s\n", found.location, found.message)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
	fmt.Println("nonetwork: no outbound network use found")
}

func check(root string) ([]finding, error) {
	var findings []finding
	goFindings, err := checkGoSources(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, goFindings...)
	moduleFindings, err := checkModules(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	findings = append(findings, moduleFindings...)
	assetFindings, err := checkFrontend(filepath.Join(root, "web"))
	if err != nil {
		return nil, err
	}
	findings = append(findings, assetFindings...)
	sort.Slice(findings, func(first, second int) bool {
		return findings[first].location < findings[second].location
	})
	return findings, nil
}

// shippedGoFile excludes tests, dev tools and the frontend's node_modules.
func shippedGoFile(relative string) bool {
	slashed := filepath.ToSlash(relative)
	return strings.HasSuffix(slashed, ".go") && !strings.HasSuffix(slashed, "_test.go") &&
		!strings.HasPrefix(slashed, "tools/") && !strings.Contains(slashed, "node_modules/") &&
		!strings.Contains(slashed, "testdata/")
}

func checkGoSources(root string) ([]finding, error) {
	var findings []finding
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if entry.IsDir() || !shippedGoFile(relative) {
			return nil
		}
		fileFindings, err := checkGoFile(path, filepath.ToSlash(relative))
		findings = append(findings, fileFindings...)
		return err
	})
	return findings, err
}

func checkGoFile(path, relative string) ([]finding, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var findings []finding
	report := func(position token.Pos, message string) {
		findings = append(findings, finding{
			location: relative + ":" + strconv.Itoa(fileSet.Position(position).Line),
			message:  message})
	}

	importNames := map[string]string{}
	for _, imported := range file.Imports {
		importPath, _ := strconv.Unquote(imported.Path.Value)
		name := filepath.Base(importPath)
		if imported.Name != nil {
			name = imported.Name.Name
		}
		symbols, watched := forbiddenSymbols[importPath]
		if watched && symbols == nil {
			report(imported.Pos(), "imports "+importPath+", which is never allowed")
		}
		if watched {
			importNames[name] = importPath
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			packageName, ok := typed.X.(*ast.Ident)
			if !ok {
				return true
			}
			importPath, watched := importNames[packageName.Name]
			if !watched {
				return true
			}
			for _, symbol := range forbiddenSymbols[importPath] {
				use := importPath + "." + symbol
				if typed.Sel.Name == symbol && allowedUses[relative+":"+use] == "" {
					report(typed.Pos(), "uses "+use+", which can open outbound connections")
				}
			}
		case *ast.CallExpr:
			checkListenCall(typed, importNames, report)
		}
		return true
	})
	return findings, nil
}

// checkListenCall allows net.Listen only as net.Listen("tcp", "127.0.0.1:0").
func checkListenCall(call *ast.CallExpr, importNames map[string]string,
	report func(token.Pos, string)) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(selector.Sel.Name, "Listen") {
		return
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || importNames[packageName.Name] != "net" {
		return
	}
	if len(call.Args) == 2 {
		if literal, ok := call.Args[1].(*ast.BasicLit); ok &&
			literal.Value == `"127.0.0.1:0"` && selector.Sel.Name == "Listen" {
			return
		}
	}
	report(call.Pos(), "net listeners must be net.Listen(\"tcp\", \"127.0.0.1:0\")")
}

func checkModules(goModPath string) ([]finding, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var findings []finding
	inRequire := false
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "require ("):
			inRequire = true
			continue
		case inRequire && line == ")":
			inRequire = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimPrefix(line, "require ")
		case !inRequire:
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && !allowedModules[fields[0]] {
			findings = append(findings, finding{
				location: "go.mod:" + strconv.Itoa(lineNumber),
				message:  "module " + fields[0] + " is not on the reviewed allowlist"})
		}
	}
	return findings, scanner.Err()
}

var (
	remoteURLPattern = regexp.MustCompile(`(?i)(https?:)?//[a-z0-9.-]+\.[a-z]{2,}[^\s"'<>)]*`)
	// XML namespace identifiers appear in UI libraries' source; browsers never fetch them.
	namespacePattern = regexp.MustCompile(`^(https?:)?//www\.w3\.org/(2000/svg|1999/xlink|` +
		`1999/xhtml|1998/Math/MathML|XML/1998/namespace|2000/xmlns/?)$`)
	frontendExtensions = map[string]bool{".html": true, ".css": true, ".js": true,
		".ts": true, ".tsx": true, ".jsx": true, ".svg": true, ".json": false}
)

// checkFrontend scans sources and, when present, the built bundle for remote URLs.
func checkFrontend(webDirectory string) ([]finding, error) {
	var findings []finding
	for _, directory := range []string{"src", "dist", "index.html"} {
		target := filepath.Join(webDirectory, directory)
		if _, err := os.Stat(target); err != nil {
			continue
		}
		err := filepath.WalkDir(target, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !frontendExtensions[filepath.Ext(path)] {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, match := range remoteURLPattern.FindAllString(string(content), -1) {
				if namespacePattern.MatchString(match) || isLoopback(match) {
					continue
				}
				findings = append(findings, finding{location: filepath.ToSlash(path),
					message: "references remote URL " + match})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return findings, nil
}

func isLoopback(url string) bool {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(url, "http:"), "https:")
	return strings.HasPrefix(trimmed, "//127.0.0.1")
}
