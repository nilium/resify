// Command resify takes a resume YAML file and runs it through a template (by default index.tem), loaded from a templates
// directory. Templates can optionally emit either HTML-specific or text output. This is specifically for controlling
// context-specific escaping in templates.
//
//  $ go get github.com/nilium/resify
//
// resify understands two commands: 'render' and 'yaml'. If given the render command, it will read any YAML files given on
// the command line, after the 'render' command, and one by one render them to the output given (by default the standard
// output).
//
// If given the yaml command, resify will write an example YAML file for use with resify to the output. This can be modified
// for generating resume outputs in any text or HTML-based format.
//
// resify expects to find templates under pwd/templates with the file extension ".tem". If any templates fail to compile or
// cannot be rendered, an error is written to standard error and resify returns 1.
//
// Templates have access to any data under templates/ and all data associated with the rtype.Resume data structure.
//
// All templates, regardless of text- or HTML-based output, have the following functions available in addition to those built
// into the template packages:
//
//  embed: Load a file beneath the template directory and return its contents. This may need to be piped to either html,
//      attr, or css depending on the context.
//
//  html: In HTML output, declare that the string passed to html is safe for the HTML context.
//
//  attr: In HTML output, declare that the string passed is safe for the HTML attribute context.
//
//  css: In HTML output, declare that the string passed is safe for the CSS context.
//
//  js: In HTML output, declare that the string passed is safe for the Javascript context.
//
//  linkify: Returns the string given to it with all instances of ((URL label)) with whatever the result of using the "link"
//      template to render them is. If no "link" (not "link.tem") template is defined, the result is the label string.
//      If there is no label string, the result is some form of the URL.
//
// An example template for use with resify (as templates/index.tem):
//
//  <!DOCTYPE html>
//  <html>
//  <head>
//      <meta charset="utf-8">
//      <title>{{ .Me.Chosen }}: Resume</title>
//  </head>
//  <body>
//      <h1>{{ .Me.Chosen }}</h1>
//      {{ if .Meta.statement }}<p>{{ .Meta.statement }}</p>{{ end }}
//
//      <h2>Employment</h2>
//      <ul>{{ range $e := .Employment }}
//          <li>
//              <h3>{{ .Title }}</h3>
//              <p>{{ .Where.Name }} ({{ .Where.Place }})</p>
//              <p>{{ .Description | linkify }}</p>
//          </li>
//      {{ end }}</ul>
//
//      <h2>Education</h2>
//      <ul>{{ range $e := .Education }}
//          <li>
//              <h3>{{ .Where.Name }} ({{ .Where.Place }})</h3>
//              <p>{{ .Where.Place }}</p>
//              <p><em>{{ or .Received "No degree" }}.</em></p>
//              {{ if .Fields }}
//              <p>Studied {{ range $nth, $f := .Fields }}{{ if gt $nth 0 }}, {{ end }}<em>{{ . }}</em>{{ end }}</p>
//              {{ end }}
//              <p>{{ .Description | linkify }}</p>
//          </li>
//      {{ end }}</ul>
//  </body>
//  </html>
//
// The only noteworthy part of the above template is the Meta.statement block -- most, but not all, data in the YAML file
// given can also have associated metadata that may be used to populate fields that may be specialized/esoteric (e.g., your
// manager's name, a note about some unusual thing, etc.).
//
// This is obviously not the be-all-end-all of tools for separating resume data and rendition, it's just good enough for my
// purposes, which is mainly so I don't have to update three different formats all the time. At most, I need to update the
// data, and then any change in format can be handled by a template.
//
// Some massaging of the data that I intended to have but didn't do because it didn't really make sense, like filling in empty
// fields based on others if you didn't provide one, are on the TODO list and probably won't actually happen unless there's
// some reason I need them.
package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nilium/resify/rtype"

	htmlt "html/template"
	textt "text/template"

	"gopkg.in/yaml.v2"
)

const whitespace = "\r\n\t "

var (
	errNotALink      = errors.New("not a link")
	errEscapeAttempt = errors.New("attempt to leave data directory via embed")
)

var dataDir = filepath.Join("templates/")

type template interface {
	ExecuteTemplate(io.Writer, string, interface{}) error
}

var formatter template

var linkFormat = regexp.MustCompile(`\(\(.+?\)\)`)

type Link struct {
	URL   *url.URL
	Label string
}

func parseLink(p string) (link Link, err error) {
	if !strings.HasPrefix(p, "((") || !strings.HasSuffix(p, "))") || len(p) <= 4 {
		return Link{}, errNotALink
	}
	p = strings.Trim(p[2:len(p)-2], whitespace)
	if len(p) == 0 {
		return Link{}, errNotALink
	}

	components := strings.SplitN(p, " ", 2)
	link.URL, err = url.Parse(strings.Trim(components[0], whitespace))
	if err != nil {
		log.Printf("error parsing link %q: %v", components[0], err)
		return Link{}, err
	}

	if len(components) > 1 {
		link.Label = strings.Trim(components[1], whitespace)
	}

	if len(link.Label) == 0 {
		link.Label = link.URL.Host + link.URL.Path
	}

	if len(link.Label) == 0 {
		link.Label = link.URL.String()
	}

	return link, err
}

// renderLink renders a link of the form ((URL label)) using the program's "link" template (it must be defined in one of the
// loaded template files). If a link cannot be rendered, the label text alone is returned. If the link cannot be parsed at all,
// the original string is returned.
//
// If there is no label, the URL's hostname (sans port) and path is used as the label. If the URL has no hostname nor path,
// besides that being weird, the full URL will be used.
func renderLink(p string, t template) (string, error) {
	link, err := parseLink(p)
	if err != nil {
		return p, err
	}

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "link", link); err != nil {
		log.Println("error rendering link:", err)
		return link.Label, err
	} else {
		return buf.String(), nil
	}
}

// linkify converts any links of the format ((URL label)) to links in the template by passing them all through the template's
// "link" template and returning the result. Non-link text is escaped and returned before re-inserting rendered links back into
// the text. Escaping only affects HTML output.
func linkify(s string) string {
	repls := map[string]string{}
	s = linkFormat.ReplaceAllStringFunc(s, func(p string) string {
		sum := sha1.Sum([]byte(p))
		id := "$" + hex.EncodeToString(sum[:]) + "$"
		if _, ok := repls[id]; ok {
			return id
		}

		l, err := renderLink(p, formatter)
		if err != nil {
			return l
		}
		repls[id] = l

		return id
	})

	s = escape(s)
	for id, link := range repls {
		s = strings.Replace(s, id, link, -1)
	}

	return s
}

func readFile(path string) (string, error) {
	path = filepath.Clean(path)
	if path == ".." || strings.HasPrefix(path, "../") {
		return "", errEscapeAttempt
	}
	b, err := ioutil.ReadFile(filepath.Join(dataDir, path))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readResumeFromFile(path string) (resume rtype.Resume, err error) {
	var b []byte
	name := path
	if path == "-" || path == "" {
		name = "stdin"
		b, err = ioutil.ReadAll(os.Stdin)
	} else {
		b, err = ioutil.ReadFile(path)
	}

	if err != nil {
		log.Printf("cannot read %s: %v", name, err)
		return
	}

	if err = yaml.Unmarshal(b, &resume); err != nil {
		log.Println("cannot parse", name, "as YAML:", err)
		resume = rtype.Resume{}
	}

	return resume, err
}

func generateYAML(w io.Writer) error {
	date, err := rtype.NewDateRange("2010-08", "2015-12")
	if err != nil {
		return err
	}

	resume := rtype.Resume{
		Me: rtype.Me{
			Order:  []string{"Chosen", "Ordered", "Name"},
			Chosen: "Chosen Name",
			Phone:  "+12345678901",
			Email:  "you@hostname.tld",
		},

		Profiles: rtype.Profiles{
			Order: []string{"github", "twitter"},
			Profile: map[string]rtype.Profile{
				"github": {
					URL:   "https://github.com/username",
					Label: "GitHub",
				},
				"twitter": {
					URL:   "https://twitter.com/username",
					Label: "Twitter",
				},
			},
		},

		Employment: []rtype.Employment{
			{
				Title: "Software Engineer",
				When:  date,
				Where: rtype.Place{
					Name:  "Foobiz Studios",
					Place: "Deadtown, AL",
				},
				Description: `I did some work for this place but it was in Alabama so ` +
					`I just felt bad the entire time, like Alabama would replace ` +
					`my liver with centipedes and upon my ribs inscribe ` +
					`the word "death". I also built distributed, high-throughput ` +
					`servers that accepted approx. 5 billion requests per day.`,

				Meta: map[string]interface{}{
					"manager": "Damien V. Satansteeth",
				},
			},
		},

		Education: []rtype.Education{
			{
				When: date,
				Where: rtype.Place{
					Name:  "Some Fake University State",
					Place: "Deadtown, AL",
				},
				Received:    "Degrees in History and Electrical Engineering", // I couldn't find a way to make this not dry.
				Fields:      []string{"History", "Electrical Engineering"},
				Description: "A description of acheivements at this institution like maybe you won an award who knows.",
			},
		},
	}

	b, err := yaml.Marshal(resume)
	if err != nil {
		return err
	}

	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil && err != io.ErrShortWrite {
			return err
		}
		b = b[n:]
	}

	return nil
}

func nopstring(s string) string { return s }

var escape = nopstring

const (
	modeYAML   int = iota // Write a YAML file to the output path and exit
	modeRender            // Parse YAML and render
)

func main() {
	rc := 0
	defer func() {
		os.Exit(rc)
	}()

	log.SetFlags(0)

	useText := false
	mainTemplate := "index.tem"
	outputPath := "-"
	newline := true

	flag.StringVar(&mainTemplate, "template", mainTemplate, "the `template` to execute")
	flag.StringVar(&dataDir, "data-dir", dataDir, "`directory` containing templates and other data")
	flag.StringVar(&outputPath, "o", outputPath, "`path` to write output to. defaults to stdout (- or empty string).")
	flag.BoolVar(&useText, "text", false, "whether to skip HTML-specific encoding in templates")
	flag.BoolVar(&newline, "newline", true, "whether to write a trailing newline at the end of each output (per YAML file)")
	flag.Parse()

	if flag.NArg() == 0 {
		log.Println("no command given, exiting with status 1")
		rc = 1
		return
	}

	var mode int
	switch flag.Arg(0) {
	case "render":
		mode = modeRender
	case "yaml":
		mode = modeYAML
	default:
		log.Printf("unrecognized command: %q", flag.Arg(0))
		rc = 1
		return
	}

	var output io.Writer = os.Stdout
	switch outputPath {
	case "", "-":
	// Stdout - default
	default:
		if fi, err := os.Create(outputPath); err != nil {
			log.Printf("cannot open %s for writing: %v", outputPath, err)
			rc = 1
			return
		} else {
			defer func() {
				if err := fi.Close(); err != nil {
					log.Printf("warning: unable to close %s on shutdown: %v", err)
				}
			}()
			output = fi
		}
	}

	defer func() {
		if rc != 1 && newline {
			io.WriteString(output, "\n")
		}
	}()

	if mode == modeYAML {
		if err := generateYAML(output); err != nil {
			rc = 1
		}
		return
	}

	if useText {
		tx, err := textt.New("root").
			Funcs(map[string]interface{}{
			"embed":   readFile,
			"html":    nopstring,
			"attr":    nopstring,
			"css":     nopstring,
			"js":      nopstring,
			"linkify": linkify,
		}).
			ParseGlob(filepath.Join(dataDir, "*.tem"))

		if err != nil {
			log.Println("error parsing template as text:", err)
			rc = 1
			return
		}

		formatter = tx
	} else {
		tx, err := htmlt.New("root").
			Funcs(map[string]interface{}{
			"embed":   readFile,
			"html":    func(s string) htmlt.HTML { return htmlt.HTML(s) },
			"attr":    func(s string) htmlt.HTMLAttr { return htmlt.HTMLAttr(s) },
			"css":     func(s string) htmlt.CSS { return htmlt.CSS(s) },
			"js":      func(s string) htmlt.JS { return htmlt.JS(s) },
			"linkify": func(s string) htmlt.HTML { return htmlt.HTML(linkify(s)) },
		}).
			ParseGlob(filepath.Join(dataDir, "*.tem"))

		if err != nil {
			log.Println("error parsing template as html:", err)
			rc = 1
			return
		}

		escape = htmlt.HTMLEscapeString
		formatter = tx
	}

	args := flag.Args()[1:]
	if len(args) == 0 {
		args = []string{"-"}
	}

	for _, arg := range args {
		resume, err := readResumeFromFile(arg)
		if err != nil {
			rc = 1
			return
		}

		var buf bytes.Buffer
		if err = formatter.ExecuteTemplate(&buf, mainTemplate, resume); err != nil {
			log.Println("cannot execute template:", err)
			rc = 1
			return
		}

		b := bytes.Trim(buf.Bytes(), whitespace)
		for len(b) > 0 {
			n, err := output.Write(b)
			if err != nil && err != io.ErrShortWrite {
				log.Println("cannot write to output:", err)
				rc = 1
				return
			}
			b = b[n:]
		}
	}
}
