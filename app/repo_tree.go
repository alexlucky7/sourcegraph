package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/sourcegraph/mux"

	"src.sourcegraph.com/sourcegraph/app/internal/tmpl"
	"src.sourcegraph.com/sourcegraph/app/router"
	"src.sourcegraph.com/sourcegraph/doc"
	"src.sourcegraph.com/sourcegraph/errcode"
	"src.sourcegraph.com/sourcegraph/go-sourcegraph/sourcegraph"
	"src.sourcegraph.com/sourcegraph/ui/payloads"
	"src.sourcegraph.com/sourcegraph/util/cacheutil"
	"src.sourcegraph.com/sourcegraph/util/eventsutil"
	"src.sourcegraph.com/sourcegraph/util/handlerutil"
	"src.sourcegraph.com/sourcegraph/util/httputil/httpctx"
)

// repoTreeTemplate holds data necessary to populate the repository tree templates
// (file and directory)
type repoTreeTemplate struct {
	*handlerutil.RepoCommon
	*handlerutil.RepoRevCommon
	*handlerutil.TreeEntryCommon
	tmpl.Common `json:"-"`

	Definition      *payloads.DefCommon
	RobotsIndex     bool
	Title           string
	MetaDescription string
	EntryPath       string
	Documentation   string
}

// serveRepoTree creates a new response for the code view that contains information
// about the requested tree entry.
func serveRepoTree(w http.ResponseWriter, r *http.Request) error {
	opt := sourcegraph.RepoTreeGetOptions{
		TokenizedSource: !doc.IsFormattableDocFile(mux.Vars(r)["Path"]) || router.IsRaw(r.URL),

		GetFileOptions: sourcegraph.GetFileOptions{
			RecurseSingleSubfolderLimit: 200,
		},
	}
	tc, rc, vc, err := handlerutil.GetTreeEntryCommon(r, &opt)
	if err != nil {
		return err
	}

	// Redirect root dir to repo homepage, so that we don't have two pages with
	// basically the same purpose and contents.
	if tc.EntrySpec.Path == "." {
		dst, err := router.Rel.URLToRepoRev(tc.EntrySpec.RepoRev.URI, tc.EntrySpec.RepoRev.Rev)
		if err != nil {
			return err
		}
		http.Redirect(w, r, dst.String(), http.StatusMovedPermanently)
		return nil
	}

	switch tc.Entry.Type {
	case sourcegraph.DirEntry:
		return serveRepoTreeDir(w, r, tc, rc, vc)
	case sourcegraph.FileEntry:
		return serveRepoTreeEntry(w, r, tc, rc, vc, nil)
	}
	return &errcode.HTTPErr{Status: http.StatusBadRequest, Err: errors.New("unrecognized tree entry type")}
}

func serveRepoTreeDir(w http.ResponseWriter, r *http.Request, tc *handlerutil.TreeEntryCommon, rc *handlerutil.RepoCommon, vc *handlerutil.RepoRevCommon) error {
	cl := handlerutil.APIClient(r)
	ctx := httpctx.FromRequest(r)
	go cacheutil.PrecacheTreeEntry(cl, ctx, tc.Entry, tc.EntrySpec)

	eventsutil.LogBrowseCode(ctx, "dir", tc, rc)
	return tmpl.Exec(r, w, "repo/tree/dir.html", http.StatusOK, nil, &repoTreeTemplate{
		TreeEntryCommon: tc,
		RepoCommon:      rc,
		RepoRevCommon:   vc,
		EntryPath:       tc.EntrySpec.Path,
	})
}

func serveRepoTreeEntry(w http.ResponseWriter, r *http.Request, tc *handlerutil.TreeEntryCommon, rc *handlerutil.RepoCommon, vc *handlerutil.RepoRevCommon, dc *payloads.DefCommon) error {
	var (
		templateFile string
		docs         string
		fullWidth    bool
	)
	ctx := httpctx.FromRequest(r)

	switch {
	case tc.Entry.Type == sourcegraph.DirEntry:
		treeURL := router.Rel.URLToRepoTreeEntrySpec(tc.EntrySpec).String()
		http.Redirect(w, r, treeURL, http.StatusFound)
		return nil
	case tc.Entry.SourceCode != nil:
		fullWidth = true
		eventsutil.LogBrowseCode(ctx, "file", tc, rc)
		templateFile = "repo/tree/file.html"
	default:
		if tc.Entry.Contents == nil {
			panic("Entry.Contents is nil")
		}
		formatted, err := doc.ToHTML(doc.Format(tc.EntrySpec.Path), tc.Entry.Contents)
		if err != nil {
			return err
		}
		docs = string(formatted)
		eventsutil.LogBrowseCode(ctx, "doc", tc, rc)
		templateFile = "repo/tree/doc.html"
	}

	return tmpl.Exec(r, w, templateFile, http.StatusOK, nil, &repoTreeTemplate{
		Common:          tmpl.Common{FullWidth: fullWidth},
		Definition:      dc,
		TreeEntryCommon: tc,
		RepoCommon:      rc,
		RepoRevCommon:   vc,
		Documentation:   docs,
		EntryPath:       tc.EntrySpec.Path,
	})
}

// FileToBreadcrumb returns the file link without line number
func FileToBreadcrumb(repoURI string, rev, path string) []*BreadcrumbLink {
	return FileLinesToBreadcrumb(repoURI, rev, path, 0)
}

// FileLinesToBreadcrumb returns the file link with line number
func FileLinesToBreadcrumb(repo string, rev, path string, startLine int) []*BreadcrumbLink {
	return SnippetToBreadcrumb(repo, rev, path, startLine, 0, nil)
}

func SnippetToBreadcrumb(repo string, rev, path string, startLine int, endLine int, defURL *url.URL) []*BreadcrumbLink {
	return AbsSnippetToBreadcrumb(nil, repo, rev, path, startLine, endLine, defURL, true)
}

type BreadcrumbLink struct {
	Text string
	URL  string
}

// AbsSnippetToBreadcrumb returns the breadcrumb to a specific set of lines in a file. The URLs are absolute if appURL is given.
func AbsSnippetToBreadcrumb(appURL *url.URL, repo string, rev, path string, startLine int, endLine int, defURL *url.URL, includeRepo bool) []*BreadcrumbLink {
	curPath := ""
	if appURL != nil {
		curPath = appURL.String()
	}
	curPath += router.Rel.URLToRepoTreeEntry(repo, rev, "").Path

	var links []*BreadcrumbLink

	if includeRepo {
		links = []*BreadcrumbLink{{Text: filepath.Base(repo), URL: curPath}}
	}

	if path == "." {
		return links
	}

	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, seg := range segs {
		link := &BreadcrumbLink{Text: seg, URL: curPath + seg}
		if i == len(segs)-1 && startLine != 0 {
			if endLine == 0 {
				endLine = startLine
			}

			link.Text += fmt.Sprintf(":%d", startLine)
			if endLine != startLine {
				link.Text += fmt.Sprintf("-%d", endLine)
			}

			link.URL += fmt.Sprintf("?startline=%d&endline=%d", startLine, endLine)
			if defURL != nil {
				link.URL += fmt.Sprintf("&seldef=%s", defURL)
			}
		}

		links = append(links, link)
		curPath += seg + "/"
	}

	return links
}
