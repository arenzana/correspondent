// Package opml reads and writes subscription lists.
package opml

import (
	"encoding/xml"
	"io"
	"time"
)

type Outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Outlines []Outline `xml:"outline"`
}

type document struct {
	XMLName xml.Name  `xml:"opml"`
	Version string    `xml:"version,attr"`
	Title   string    `xml:"head>title"`
	Created string    `xml:"head>dateCreated,omitempty"`
	Body    []Outline `xml:"body>outline"`
}

// Subscription is one feed with the folder it belongs to ("" for none).
type Subscription struct {
	Folder  string
	Title   string
	URL     string
	SiteURL string
}

// Parse flattens an OPML document; nested folders collapse to their
// outermost name.
func Parse(r io.Reader) ([]Subscription, error) {
	var doc document
	dec := xml.NewDecoder(io.LimitReader(r, 10<<20))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var out []Subscription
	var walk func(folder string, os []Outline)
	walk = func(folder string, os []Outline) {
		for _, o := range os {
			title := o.Title
			if title == "" {
				title = o.Text
			}
			if o.XMLURL != "" {
				out = append(out, Subscription{Folder: folder, Title: title, URL: o.XMLURL, SiteURL: o.HTMLURL})
				continue
			}
			f := folder
			if f == "" {
				f = title
			}
			walk(f, o.Outlines)
		}
	}
	walk("", doc.Body)
	return out, nil
}

func Write(w io.Writer, title string, subs []Subscription) error {
	doc := document{Version: "2.0", Title: title, Created: time.Now().UTC().Format(time.RFC1123Z)}
	folders := map[string]int{}
	for _, s := range subs {
		o := Outline{Text: s.Title, Title: s.Title, Type: "rss", XMLURL: s.URL, HTMLURL: s.SiteURL}
		if s.Folder == "" {
			doc.Body = append(doc.Body, o)
			continue
		}
		i, ok := folders[s.Folder]
		if !ok {
			i = len(doc.Body)
			folders[s.Folder] = i
			doc.Body = append(doc.Body, Outline{Text: s.Folder, Title: s.Folder})
		}
		doc.Body[i].Outlines = append(doc.Body[i].Outlines, o)
	}
	io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(doc)
}
