package httpapi

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func sanitizeFooter(input string) (string, error) {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Footer, Data: "footer"}
	nodes, err := html.ParseFragment(strings.NewReader(input), context)
	if err != nil {
		return "", err
	}
	var safe strings.Builder
	for _, node := range nodes {
		writeFooterNode(&safe, node)
	}
	return strings.TrimSpace(safe.String()), nil
}

func writeFooterNode(out *strings.Builder, node *html.Node) {
	if node.Type == html.TextNode {
		out.WriteString(html.EscapeString(node.Data))
		return
	}
	if node.Type != html.ElementNode || node.Namespace != "" {
		return
	}
	name := node.Data
	switch node.DataAtom {
	case atom.Script, atom.Style, atom.Template, atom.Iframe, atom.Object:
		return
	case atom.Br:
		out.WriteString("<br>")
		return
	case atom.A:
		href, blank := "", false
		for _, attr := range node.Attr {
			switch attr.Key {
			case "href":
				link, err := url.Parse(strings.TrimSpace(attr.Val))
				if err == nil && (link.Scheme == "http" || link.Scheme == "https") && link.Hostname() != "" {
					href = link.String()
				}
			case "target":
				blank = attr.Val == "_blank"
			}
		}
		if href == "" {
			name = ""
		} else {
			out.WriteString(`<a href="` + html.EscapeString(href) + `"`)
			if blank {
				out.WriteString(` target="_blank" rel="noopener noreferrer"`)
			}
			out.WriteByte('>')
		}
	case atom.B, atom.Em, atom.I, atom.P, atom.Small, atom.Span, atom.Strong:
		out.WriteString("<" + name + ">")
	default:
		name = ""
	}
	for child := range node.ChildNodes() {
		writeFooterNode(out, child)
	}
	if name != "" {
		out.WriteString("</" + name + ">")
	}
}
