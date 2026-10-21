package generator

// view_list.go turns a schema's spec.ListResources into the strings list.go.tmpl
// writes. As in view_resource.go, every Go identifier is decided here.

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/the-protobuf-project/protokit/naming"

	"github.com/the-protobuf-project/cache/plugin/spec"
)

// listFileView is one schema's list.go.
type listFileView struct {
	Banner      string
	Package     string
	Imports     []string
	RepoPackage string
	Lists       []listView
}

// listView is one resource's list cache.
type listView struct {
	Node             string
	Model            string
	Pattern          string
	CacheType        string // ReservationListCache
	Wrapper          string // cachedReservationList
	Iface            string // reservation.ReservationRepository
	PB               string // reservationpbv1.Reservation
	Parented         bool
	CollectionsVar   string
	CollectionsLit   string
	NamespaceConst   string
	NamespaceLiteral string
	TTL              string
}

func buildListFile(banner, repositoryModule, database string, s *spec.Schema, msgs map[string]goMessage) (listFileView, error) {
	repoPkg := naming.GoPackage(s.Name)
	repoImport := path.Join(repositoryModule, database, repoPkg)
	f := listFileView{
		Banner:      banner,
		Package:     s.GoPackage,
		RepoPackage: repoPkg,
	}
	// Imports by path, each named by the package it declares: no aliases, so
	// two that declare one name are refused rather than renamed.
	names := map[string]string{
		repoImport:                           repoPkg,
		path.Join(repositoryModule, "repox"): "repox",
	}
	for _, l := range s.Lists {
		m, ok := msgs[string(l.Node)]
		if !ok {
			return f, fmt.Errorf("cache: no generated Go type for %s (is its proto in the codegen request?)", l.Node)
		}
		pbPath, pbName := m.ImportPath, m.Package
		names[pbPath] = pbName
		lower := lowerFirst(l.Model)
		quoted := make([]string, len(l.Collections))
		for i, c := range l.Collections {
			quoted[i] = strconv.Quote(c)
		}
		f.Lists = append(f.Lists, listView{
			Node:             string(l.Node),
			Model:            l.Model,
			Pattern:          l.Pattern,
			CacheType:        l.Model + "ListCache",
			Wrapper:          "cached" + l.Model + "List",
			Iface:            repoPkg + "." + l.Model + "Repository",
			PB:               pbName + "." + m.Name,
			Parented:         l.Parented,
			CollectionsVar:   lower + "Collections",
			CollectionsLit:   "[]string{" + strings.Join(quoted, ", ") + "}",
			NamespaceConst:   lower + "ListNamespace",
			NamespaceLiteral: strconv.Quote(l.Namespace),
			TTL:              durationLit(l.TTL),
		})
	}
	byName := map[string]string{}
	var third []string
	for p, n := range names {
		if other, ok := byName[n]; ok {
			return f, fmt.Errorf("cache: %s/%s: %s and %s both declare package %s, and the "+
				"generated list.go names every import by its own package name", database, s.GoPackage, other, p, n)
		}
		byName[n] = p
		third = append(third, strconv.Quote(p))
	}
	third = append(third,
		strconv.Quote("github.com/the-protobuf-project/runtime-go/cache"),
		strconv.Quote("google.golang.org/protobuf/proto"))
	sort.Strings(third)
	f.Imports = third
	return f, nil
}
