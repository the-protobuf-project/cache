// Package generator assembles protoc-gen-cache's protokit.Plugin: the target
// registry, the reader set, and the layout policy.
//
// It is a package of its own rather than a constructor in the root, because the
// root package is the cache.v1 reader and spec imports it. A Plugin constructor
// up there would close the loop root → target → spec → root. Keeping the
// assembly here is what lets the reader stay importable by everything that needs
// it without dragging the renderer along.
//
// "generator" rather than the more obvious "wire" because protoc-gen-store has a
// package of that name, and the agreement test holds both at once. A name that
// forces every joint consumer to alias one of them is a name worth spending a
// few characters to avoid.
//
// One constructor is used by the plugin binary and by every test, so a golden
// run and a real run differ in their inputs and in nothing else.
package generator

import (
	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/cachev1"
)

// Modules are the Go import paths of the protoc-gen-store outputs the
// generated code imports. They are paths rather than dependencies: the two
// plugins never see each other, and these are the only thing joining them.
type Modules struct {
	// Store is protoc-gen-store's gorm output (store_module), whose
	// <Model>StoreIface the record decorators wrap.
	Store string

	// Repository is protoc-gen-store's repository output (repository_module),
	// whose <Model>Repository the list caches wrap. Needed only when a
	// resource caches its list.
	Repository string
}

// Registry is the target set this plugin ships.
func Registry(mods Modules, versions Versions) map[string]schema.Target {
	return map[string]schema.Target{
		TargetName: New(mods, versions),
	}
}

// Plugin is the whole generator: targets, readers, layout.
//
// Readers comes from cachev1.Readers(), which pairs this plugin's own reader with
// entity.Reader() imported from the store repository. That import is the entire
// mechanism behind golden.IRAgreement — see the comment on cache.Readers.
func Plugin(mods Modules, versions Versions, layout protokit.LayoutResolver) protokit.Plugin {
	return protokit.Plugin{
		Registry: Registry(mods, versions),
		Readers:  cachev1.Readers(),
		Layout:   layout,
	}
}
