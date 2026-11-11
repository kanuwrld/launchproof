package workflow

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/kanuwrld/launchproof/internal/project"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type DocIssue struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
type DocsReport struct {
	Files   int        `json:"files"`
	Links   int        `json:"links_checked"`
	Scripts int        `json:"scripts_checked"`
	Issues  []DocIssue `json:"issues"`
}

type docLink struct {
	destination string
	position    int
}

var htmlLink = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*["']([^"']+)["']`)
var htmlID = regexp.MustCompile(`(?i)(?:id|name)\s*=\s*["']([^"']+)["']`)
var scriptRef = regexp.MustCompile(`\b(?:npm|pnpm|yarn|bun)\s+run\s+([A-Za-z0-9][A-Za-z0-9:_-]*)`)

func parsedMarkdown(source []byte) ([]docLink, map[string]bool) {
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	links := []docLink{}
	anchors := map[string]bool{}
	addHTML := func(data []byte, position int) {
		for _, match := range htmlLink.FindAllSubmatch(data, -1) {
			links = append(links, docLink{html.UnescapeString(string(match[1])), position})
		}
		for _, match := range htmlID.FindAllSubmatch(data, -1) {
			anchors[html.UnescapeString(string(match[1]))] = true
		}
	}
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Link:
			links = append(links, docLink{string(n.Destination), n.Pos()})
		case *ast.Image:
			links = append(links, docLink{string(n.Destination), n.Pos()})
		case *ast.RawHTML:
			addHTML(n.Segments.Value(source), n.Pos())
		case *ast.HTMLBlock:
			addHTML(n.Lines().Value(source), n.Pos())
		case *ast.Heading:
			var title strings.Builder
			_ = ast.Walk(n, func(child ast.Node, enter bool) (ast.WalkStatus, error) {
				if enter {
					if t, ok := child.(*ast.Text); ok {
						title.Write(t.Segment.Value(source))
					}
					if t, ok := child.(*ast.String); ok {
						title.Write(t.Value)
					}
				}
				return ast.WalkContinue, nil
			})
			slug := strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' {
					return unicode.ToLower(r)
				}
				if unicode.IsSpace(r) {
					return '-'
				}
				return -1
			}, strings.TrimSpace(html.UnescapeString(title.String())))
			base := slug
			for count := 1; anchors[slug]; count++ {
				slug = base + "-" + strconv.Itoa(count)
			}
			anchors[slug] = true
		}
		return ast.WalkContinue, nil
	})
	return links, anchors
}

func CheckDocs(ctx context.Context, root string) (DocsReport, error) {
	result := DocsReport{Issues: []DocIssue{}}
	files, err := SourceFiles(ctx, root, nil)
	if err != nil {
		return result, err
	}
	scope, err := os.OpenRoot(root)
	if err != nil {
		return result, err
	}
	defer scope.Close()
	anchors := map[string]map[string]bool{}
	report, inspectErr := project.Inspect(ctx, root)
	_, manifestErr := ReadFile(root, "package.json", 1<<20)
	hasNode := inspectErr == nil && manifestErr == nil
	for _, name := range files {
		if !strings.EqualFold(path.Ext(name), ".md") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if result.Files >= 500 {
			return result, fmt.Errorf("more than 500 Markdown files; select an application directory")
		}
		source, err := ReadFile(root, name, 2<<20)
		if err != nil {
			result.Issues = append(result.Issues, DocIssue{name, 1, "file", err.Error()})
			continue
		}
		result.Files++
		links, localAnchors := parsedMarkdown(source)
		anchors[name] = localAnchors
		for _, link := range links {
			destination, err := url.Parse(html.UnescapeString(link.destination))
			if err != nil {
				result.Issues = append(result.Issues, DocIssue{name, 1, "link", "Invalid URL: " + link.destination})
				continue
			}
			if destination.Scheme != "" || destination.Host != "" || strings.HasPrefix(link.destination, "//") {
				continue
			}
			line := 1 + strings.Count(string(source[:min(max(0, link.position), len(source))]), "\n")
			target := name
			if destination.Path != "" {
				if strings.HasPrefix(destination.Path, "/") {
					target = strings.TrimPrefix(path.Clean(destination.Path), "/")
				} else {
					target = path.Clean(path.Join(path.Dir(name), destination.Path))
				}
			}
			result.Links++
			info, err := scope.Stat(target)
			if err != nil {
				result.Issues = append(result.Issues, DocIssue{name, line, "link", "Missing or inaccessible local target: " + link.destination})
				continue
			}
			if destination.Fragment != "" && !info.IsDir() && strings.EqualFold(path.Ext(target), ".md") {
				known, ok := anchors[target]
				if !ok {
					data, readErr := ReadFile(root, target, 2<<20)
					if readErr != nil {
						result.Issues = append(result.Issues, DocIssue{name, line, "link", readErr.Error()})
						continue
					}
					_, known = parsedMarkdown(data)
					anchors[target] = known
				}
				if !known[destination.Fragment] {
					result.Issues = append(result.Issues, DocIssue{name, line, "anchor", "Missing Markdown anchor: " + link.destination})
				}
			}
		}
		if hasNode {
			for index, line := range strings.Split(string(source), "\n") {
				for _, match := range scriptRef.FindAllStringSubmatch(line, -1) {
					result.Scripts++
					if !slices.Contains(report.Scripts, match[1]) {
						result.Issues = append(result.Issues, DocIssue{name, index + 1, "script", "Script not declared in package.json: " + match[1]})
					}
				}
			}
		}
	}
	return result, nil
}

func DocsText(report DocsReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "DOCUMENTATION CHECK\n\n%d Markdown files · %d local links · %d script references\n", report.Files, report.Links, report.Scripts)
	if len(report.Issues) == 0 {
		if report.Files == 0 {
			b.WriteString("No Markdown files found.\n")
		} else {
			b.WriteString("No broken local references found.\n")
		}
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(&b, "\n%s:%d [%s] %s\n", CleanText(issue.File), issue.Line, issue.Kind, CleanText(issue.Message))
	}
	b.WriteString("\nExternal URLs and commands other than explicit package-manager run references are not checked.\n")
	return b.String()
}
