package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"git.zexi.me/zexi/atrace/server/auth"
	"git.zexi.me/zexi/atrace/server/core"
)

const archiveRoot = "/atrace"

type archive struct {
	ctx        context.Context
	renderer   *Renderer
	base       string
	owner      bool
	entries    map[string]string
	dirs       map[string][]string
	current    *archivePost
	currentDir string
}

type archivePost struct {
	item  core.Item
	files map[string]archiveFile
	entry *Entry
}

type archiveFile struct {
	ref     string
	url     string
	private bool
}

// The archive's lightweight directory index lives for one command request.
func (a *archive) load() error {
	if a.entries != nil {
		return nil
	}
	entries, err := a.renderer.feed.ArchiveEntries(a.ctx, !a.owner, "")
	if err != nil {
		return err
	}
	a.entries = make(map[string]string, len(entries))
	dirs := map[string]map[string]bool{archiveRoot: {"README.md": true}}
	for _, entry := range entries {
		dir := archiveRoot
		for _, name := range strings.Split(strings.TrimPrefix(a.entryPath(entry), archiveRoot+"/"), "/") {
			if dirs[dir] == nil {
				dirs[dir] = map[string]bool{}
			}
			dirs[dir][name] = true
			dir += "/" + name
		}
		a.entries[dir] = entry.Ref
	}
	a.dirs = make(map[string][]string, len(dirs))
	for dir, children := range dirs {
		for name := range children {
			a.dirs[dir] = append(a.dirs[dir], name)
		}
		sort.Strings(a.dirs[dir])
	}
	return nil
}

func archivePath(cwd, name string) (string, error) {
	if name == "" || strings.ContainsFunc(name, unicode.IsControl) {
		return "", fs.ErrInvalid
	}
	if !strings.HasPrefix(name, "/") {
		name = cwd + "/" + name
	}
	name = path.Clean(name)
	if name == "/" {
		return archiveRoot, nil
	}
	if name != archiveRoot && !strings.HasPrefix(name, archiveRoot+"/") {
		return "", fs.ErrNotExist
	}
	return name, nil
}

// entryPath preserves readable names and escapes characters that have path semantics.
func (a *archive) entryPath(entry core.ArchiveEntry) string {
	if !a.owner {
		return archiveRoot + "/" + entry.Month + "/" + entry.Ref
	}
	person := strings.NewReplacer("%", "%25", "/", "%2F").Replace(entry.Person)
	if person == "." || person == ".." || person == "README.md" {
		person = "%2E" + person
	} else if strings.ContainsFunc(person, unicode.IsControl) {
		person = url.PathEscape(entry.Person)
	}
	return archiveRoot + "/" + person + "/" + entry.Month + "/" + entry.Ref
}

func (a *archive) entryDir(name string) (string, string) {
	relative, ok := strings.CutPrefix(name, archiveRoot+"/")
	if !ok {
		return "", ""
	}
	parts := strings.Split(relative, "/")
	refIndex := 1
	if a.owner {
		refIndex = 2
	}
	if len(parts) <= refIndex || !refPattern.MatchString(parts[refIndex]) {
		return "", ""
	}
	return archiveRoot + "/" + strings.Join(parts[:refIndex+1], "/"), parts[refIndex]
}

func (a *archive) locate(name string) (*archivePost, string, error) {
	dir, ref := a.entryDir(name)
	if dir == "" {
		return nil, "", fs.ErrNotExist
	}
	if a.entries != nil {
		if a.entries[dir] != ref {
			return nil, "", fs.ErrNotExist
		}
	} else if a.currentDir != dir {
		entries, err := a.renderer.feed.ArchiveEntries(a.ctx, !a.owner, ref)
		if err != nil {
			return nil, "", err
		}
		if len(entries) != 1 || dir != a.entryPath(entries[0]) {
			return nil, "", fs.ErrNotExist
		}
	}
	post, err := a.post(ref)
	if err == nil {
		a.currentDir = dir
	}
	return post, strings.TrimPrefix(strings.TrimPrefix(name, dir), "/"), err
}

func (a *archive) list(name string) ([]string, error) {
	if dir, _ := a.entryDir(name); dir == "" {
		if err := a.load(); err != nil {
			return nil, err
		}
		if children, ok := a.dirs[name]; ok {
			return children, nil
		}
		return nil, fs.ErrNotExist
	}
	post, suffix, err := a.locate(name)
	if err != nil {
		return nil, err
	}
	if suffix == "" {
		children := []string{"entry.json", "entry.md"}
		if len(post.files) != 0 {
			children = append([]string{"attachments"}, children...)
		}
		return children, nil
	}
	children := map[string]bool{}
	for file := range post.files {
		if tail, ok := strings.CutPrefix(file, suffix+"/"); ok {
			children[strings.SplitN(tail, "/", 2)[0]] = true
		}
	}
	if len(children) == 0 {
		return nil, fs.ErrNotExist
	}
	names := make([]string, 0, len(children))
	for child := range children {
		names = append(names, child)
	}
	sort.Strings(names)
	return names, nil
}

func (a *archive) post(ref string) (*archivePost, error) {
	if a.current != nil && a.current.item.Ref == ref {
		return a.current, nil
	}
	item, err := a.renderer.feed.TimelineItem(a.ctx, ref, !a.owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	post := &archivePost{item: item, files: map[string]archiveFile{}}
	var doc document
	if item.Object != nil {
		if err := json.Unmarshal(item.Object, &doc); err != nil {
			return nil, err
		}
	}
	if len(doc.Attachment) > 0 {
		urls := []string{}
		for _, media := range doc.Attachment {
			if link := safeWebURL(media.URL); link != "" {
				urls = append(urls, link)
			}
		}
		copies, err := a.renderer.feed.MediaRefs(a.ctx, urls)
		if err != nil {
			return nil, err
		}
		for i, link := range urls {
			u, err := url.Parse(link)
			if err != nil {
				continue
			}
			name := path.Base(u.Path)
			if name == "." || name == "/" || name == "" || strings.ContainsFunc(name, unicode.IsControl) {
				name = "media"
			}
			ref := copies[link]
			if ref == "" {
				name += ".url"
			}
			filename := "attachments/" + strconv.Itoa(i+1) + "-" + name
			post.files[filename] = archiveFile{ref: ref, url: link}
		}
	}
	if a.owner && item.Activity.Kind == "Create" {
		files, err := a.renderer.feed.ArchiveAttachmentPaths(a.ctx, item.Activity.Object)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			post.files["attachments/"+file] = archiveFile{private: true}
		}
	}
	// Keep only the current post's content in memory during recursive commands.
	a.current = post
	return post, nil
}

func (a *archive) read(name string) (io.ReadCloser, bool, error) {
	text := func(value string) (io.ReadCloser, bool, error) {
		return io.NopCloser(strings.NewReader(value)), true, nil
	}
	if name == archiveRoot+"/README.md" {
		if a.owner {
			return text(ownerArchiveReadme)
		}
		return text(archiveReadme)
	}
	post, suffix, err := a.locate(name)
	if err != nil {
		return nil, false, err
	}
	switch suffix {
	case "entry.md", "entry.json":
		ctx := a.ctx
		if !a.owner {
			ctx = auth.Visitor(ctx)
		}
		if post.entry == nil {
			rendered, err := a.renderer.entryOf(ctx, post.item, map[string]string{}, true)
			if err != nil {
				return nil, false, err
			}
			entry := visibleTo(a.owner, rendered)
			post.entry = &entry
		}
		entry := *post.entry
		if suffix == "entry.md" {
			return text(entryMarkdown(visibleTo(a.owner, entry), a.base, true))
		}
		value := any(entry)
		if a.owner {
			raw, err := a.renderer.feed.LatestRaw(ctx, post.item.Activity.Object)
			if err != nil {
				return nil, false, err
			}
			value = struct {
				Entry
				Activity json.RawMessage `json:"activity"`
				Document json.RawMessage `json:"document,omitempty"`
				Raw      json.RawMessage `json:"raw,omitempty"`
			}{entry, post.item.Activity.Document, post.item.Object, raw}
		}
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, false, err
		}
		return text(string(data) + "\n")
	default:
		file, ok := post.files[suffix]
		if !ok {
			return nil, false, fs.ErrNotExist
		}
		if file.private {
			data, err := a.renderer.feed.ArchiveAttachmentText(a.ctx, post.item.Activity.Object, strings.TrimPrefix(suffix, "attachments/"))
			if err != nil {
				return nil, false, err
			}
			return text(data)
		}
		if file.ref == "" {
			return text(file.url + "\n")
		}
		reader, err := a.renderer.feed.OpenFile(file.ref)
		return reader, false, err
	}
}

func (a *archive) walk(name string, visit func(string, bool) error) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	children, err := a.list(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if err := a.fileExists(name); err != nil {
				return err
			}
			return visit(name, false)
		}
		return err
	}
	if err := visit(name, true); err != nil {
		return err
	}
	for _, child := range children {
		if err := a.walk(path.Join(name, child), visit); err != nil {
			return err
		}
	}
	return nil
}

func (a *archive) fileExists(name string) error {
	if name == archiveRoot+"/README.md" {
		return nil
	}
	post, suffix, err := a.locate(name)
	if err != nil {
		return err
	}
	if suffix == "entry.md" || suffix == "entry.json" {
		return nil
	}
	if _, ok := post.files[suffix]; ok {
		return nil
	}
	return fmt.Errorf("%s: %w", name, fs.ErrNotExist)
}
