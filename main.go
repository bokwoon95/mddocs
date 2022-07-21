package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || len(args) > 2 {
		fmt.Printf(`Usage:
%[1]s project.md              # serves project.md on a localhost connection
%[1]s project.md project.html # render project.md into project.html
`, filepath.Base(os.Args[0]))
		return
	}
	if len(args) == 2 {
		b, err := render(args[0])
		if err != nil {
			log.Fatal(err)
		}
		file, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		_, err = file.Write(b)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	var err error
	var ln net.Listener
	for i := 0; i < 10; i++ {
		ln, err = net.Listen("tcp", ":606"+strconv.Itoa(i))
		if err == nil {
			break
		}
	}
	if ln == nil {
		ln, err = net.Listen("tcp", ":0")
		if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("serving %s at localhost:%d\n", args[0], ln.Addr().(*net.TCPAddr).Port)
	http.Serve(ln, serve(args[0]))
}

type Header struct {
	Title      string
	HeaderID   string
	Level      int
	Subheaders []Header
}

func serve(filename string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := render(filename)
		if err != nil {
			fmt.Fprintln(w, err.Error())
			return
		}
		_, err = w.Write(b)
		if err != nil {
			log.Println(err)
		}
	})
}

//go:embed base.html
var basehtml string

var basetmpl = template.Must(template.New("base.html").Parse(basehtml))

func render(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	fileinfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	buf := &bytes.Buffer{}
	buf.Grow(int(fileinfo.Size() * 2))
	r := bufio.NewReader(file)
	var parents [1 + 6]*Header
	parents[0] = &Header{}
	fallbackParent := parents[0]
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(line, "#") {
			buf.WriteString(line)
			continue
		}
		headerLevel := 0
		for _, char := range line {
			if char != '#' {
				break
			}
			headerLevel++
		}
		i := strings.Index(line[headerLevel:], "#")
		if i < 0 {
			buf.WriteString(line)
			continue
		}
		headerID := strings.TrimSpace(line[headerLevel+i+1:])
		isValidID := true
		for _, char := range headerID {
			if char != '_' && char != '-' && !unicode.IsLetter(char) && !unicode.IsDigit(char) {
				isValidID = false
				break
			}
		}
		if !isValidID {
			buf.WriteString(line)
			continue
		}
		title := strings.TrimSpace(line[headerLevel:headerLevel+i])
		line2 := fmt.Sprintf(
			"%[1]s [%[2]s](#toc-%[3]s) [[link](#%[3]s)] {#%[3]s}\n",
			strings.Repeat("#", headerLevel),
			title,
			headerID,
		)
		buf.WriteString(line2)
		header := Header{
			Title:    title,
			HeaderID: headerID,
			Level:    headerLevel,
		}
		if parent := parents[headerLevel-1]; parent != nil {
			parent.Subheaders = append(parent.Subheaders, header)
			n := len(parent.Subheaders) - 1
			parents[headerLevel] = &parent.Subheaders[n]
		} else {
			fallbackParent.Subheaders = append(fallbackParent.Subheaders, header)
			n := len(fallbackParent.Subheaders) - 1
			parents[headerLevel] = &fallbackParent.Subheaders[n]
		}
		if header.Level == fallbackParent.Level+1 {
			fallbackParent = parents[headerLevel]
		}
	}
	tableOfContents := &strings.Builder{}
	tableOfContents.Grow(buf.Len()/4)
	renderTableOfContents(tableOfContents, parents[0].Subheaders)

	contents := &strings.Builder{}
	contents.Grow(buf.Len()*4)
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithAttribute(),
		),
		goldmark.WithExtensions(
			extension.Table,
			highlighting.NewHighlighting(
				highlighting.WithStyle("dracula"),
			),
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithUnsafe(),
		),
	)
	err = md.Convert(buf.Bytes(), contents)
	if err != nil {
		return nil, err
	}

	output := &bytes.Buffer{}
	output.Grow(buf.Len()*4)
	err = basetmpl.Execute(output, map[string]any{
		"Lang":            "en",
		"Title":           strings.TrimSuffix(filepath.Clean(filename), filepath.Ext(filename)),
		"TableOfContents": template.HTML(tableOfContents.String()),
		"Contents":        template.HTML(contents.String()),
	})
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func renderTableOfContents(buf *strings.Builder, headers []Header) {
	if len(headers) == 0 {
		return
	}
	buf.WriteString("<ul>")
	for _, header := range headers {
		buf.WriteString("\n<li><a" +
			` id="` + url.QueryEscape("toc-"+header.HeaderID) + `"` +
			` href="#` + url.QueryEscape(header.HeaderID) + `"` +
			`>` +
			html.EscapeString(header.Title) +
			"</a></li>",
		)
		renderTableOfContents(buf, header.Subheaders)
	}
	buf.WriteString("\n</ul>")
}
