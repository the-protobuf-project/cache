// Package provenance renders the generated-file banner with the full set of
// modules that decided the output.
//
// A banner naming only the plugin answers "why did this file change?" wrongly
// whenever the answer is something else, and for this plugin the answer is
// usually something else. Output here is decided by four independently-versioned
// things: this plugin (which owns cache.v1), the store module (which owns
// entity.v1 and the neutral reader every resource is addressed through),
// protokit (which builds the IR those names come out of), and runtime-go/cache
// (whose contracts the generated code is written against). A bump to any one of
// them can move this output without a line of this repository changing.
//
// So the banner records all four.
//
// # Why the versions are passed in rather than looked up here
//
// They used to be read from debug.ReadBuildInfo() at render time, which was
// wrong in a way that took a broken workspace to expose. A golden file is
// compared byte for byte, and two of the three version lines were resolved from
// whatever the *test binary* happened to link against — so the committed goldens
// silently recorded a build environment rather than a generator's output, and
// went red the moment the workspace resolved differently. golden_test.go pinned
// the plugin's own version for exactly this reason and the other two escaped it.
//
// Rendering is now a pure function of its inputs. Build info is read once, in
// the plugin binary, by ResolveVersions.
package generator

import (
	"runtime/debug"
	"strings"

	"github.com/the-protobuf-project/protokit/header"
)

// entityModule carries entity.v1 and the reader every protokit plugin shares.
//
// It is the store *root* module, not a nested one. entity used to be its own
// module at store/plugin/entity — published on its own tag, so its version was
// looked up separately — and the store repository has since folded it back into
// the root. The import path is unchanged; only the module that provides it moved.
// See docs/boundary-findings.md, finding 5.
const entityModule = "github.com/the-protobuf-project/store"

// protokitModule is the engine. It owns no annotations, but it still decides the
// structure — a protokit bump can move a derived name without a single
// annotation changing — so it stays on its own line.
const protokitModule = "github.com/the-protobuf-project/protokit"

// runtimeModule is what the *generated* code imports. Its version is deliberately
// absent from the banner: the consumer's go.mod resolves it, not this plugin, and
// printing the version this binary happened to build against would be a
// plausible-looking lie. The plugin does not even link against it.
const runtimeModule = "github.com/the-protobuf-project/runtime-go/cache"

// Unknown is the sentinel protoc-gen-go uses for a version it cannot determine.
const Unknown = "(unknown)"

// Versions are the module versions one run stamps into every file it writes.
//
// A zero field renders as Unknown rather than empty, so a banner never claims a
// version it does not have.
type Versions struct {
	// Plugin is this plugin's version. cache.v1 ships in this repository's root
	// module, so it carries this number by construction — there is no separate
	// one to look up, and pretending otherwise would invite the two to drift.
	Plugin string

	// Entity is the version of the module providing entity.v1 and the shared
	// reader. It does not get cache.v1's treatment: it is a separate module on a
	// separate tag, and a consumer may be running a plugin built against an
	// older one than this checkout contains.
	Entity string

	// Engine is protokit's version.
	Engine string
}

// ResolveVersions reads the dependency versions from the running binary's build
// info, given the plugin's own version resolved by the caller.
//
// This is the binary's job and no one else's: build info describes what *this
// process* was linked against, which is a fact about an invocation rather than
// about a schema. A test that called it would get the test binary's answer, which
// is why the golden harness supplies Versions directly instead.
func ResolveVersions(plugin string) Versions {
	return Versions{
		Plugin: orUnknown(plugin),
		Entity: moduleVersion(entityModule),
		Engine: moduleVersion(protokitModule),
	}
}

// moduleVersion resolves a dependency's version from the build info the Go
// toolchain embeds. A test binary, a `go run` build, or a module replaced by a
// local directory carries no version, in which case it stays unknown rather than
// being guessed at.
func moduleVersion(path string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return Unknown
	}
	for _, dep := range bi.Deps {
		if dep.Path == path && dep.Version != "" {
			return dep.Version
		}
	}
	return Unknown
}

// Render renders in's banner with v's provenance lines appended, prefixed by
// prefix ("//" for Go).
func Render(prefix string, in header.Info, v Versions) string {
	in.PluginVersion = orUnknown(v.Plugin)
	in.Notes = append(in.Notes, notes(v)...)
	return header.Render(prefix, in)
}

// notes builds the provenance lines.
func notes(v Versions) []string {
	return []string{
		"annotations: entity.v1 " + orUnknown(v.Entity) + ", cache.v1 " + orUnknown(v.Plugin),
		"engine:      protokit " + orUnknown(v.Engine),
		"runtime:     " + runtimeModule,
	}
}

func orUnknown(v string) string {
	if v == "" {
		return Unknown
	}
	return v
}

// SourceLine joins the proto import paths contributing to one file, for the
// banner's source line.
func SourceLine(protos []string) string { return strings.Join(protos, ", ") }
