// Copyright 2017 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cc

import (
	"sort"
	"strings"
	"sync"

	"android/soong/android"
)

var (
	// pagerandoCFlags = []string{}
	// pagerandoLdFlags = []string{}
	pagerandoCFlags  = []string{"-fsanitize=pagerando",
		"-fsanitize-blacklist=build/soong/cc/config/pagerando_blacklist.txt"}
	pagerandoLdFlags = []string{"-Wl,--plugin-opt,pagerando",
		"-Wl,--plugin-opt,-pagerando-skip-trivial",
		"-Wl,--plugin-opt,-pagerando-binning-strategy=pgo"}
	pagerandoStaticLibsMutex    sync.Mutex
)

type PagerandoProperties struct {
	Pagerando *bool `android:"arch_variant"`

	// Dep properties indicate that this module needs to be built with
	// pagerando since it is an object dependency of a pagerando module.
	PagerandoDep bool `blueprint:"mutated"`
}

type pagerando struct {
	Properties PagerandoProperties
}

func init() {
	android.RegisterMakeVarsProvider(pctx, pagerandoMakeVarsProvider)
}

func (pagerando *pagerando) props() []interface{} {
	return []interface{}{&pagerando.Properties}
}

func (pagerando *pagerando) begin(ctx BaseModuleContext) {
	// Pagerando should only be enabled for device builds
	if !ctx.Device() {
		pagerando.Properties.Pagerando = BoolPtr(false)
	}

	// Pagerando is currently only implemented for arm and arm64
	if ctx.Arch().ArchType != android.Arm && ctx.Arch().ArchType != android.Arm64 {
		pagerando.Properties.Pagerando = BoolPtr(false)
	}

	// Pagerando should only be enabled for libraries
	if !ctx.sharedLibrary() && !ctx.staticLibrary() {
		pagerando.Properties.Pagerando = BoolPtr(false)
	}

	// Static libraries will pick up pagerando as dependencies of a shared
	// library if needed, so we shouldn't ever mark them as explicitly
	// requiring pagerando.
	if ctx.staticLibrary() {
		pagerando.Properties.Pagerando = nil
	}

	// If local blueprint does not specify, allow global setting to enable
	// pagerando. Static libs should have both pagerando and non-pagerando
	// versions built for consumption by make.
	if ctx.sharedLibrary() && ctx.AConfig().Pagerando() &&
		pagerando.Properties.Pagerando == nil {
		pagerando.Properties.Pagerando = BoolPtr(true)
	}
}

func (pagerando *pagerando) deps(ctx BaseModuleContext, deps Deps) Deps {
	return deps
}

func (pagerando *pagerando) flags(ctx BaseModuleContext, flags Flags) Flags {
	if pagerando.Pagerando() {
		flags.CFlags = append(flags.CFlags, pagerandoCFlags...)
		flags.LdFlags = append(flags.LdFlags, pagerandoLdFlags...)
	}
	return flags
}

func (pagerando *pagerando) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkData) {
	if ret.Class == "STATIC_LIBRARIES" && pagerando.Pagerando() {
		ret.SubName += ".pagerando"
	}
}

// Can be called with a null receiver
func (pagerando *pagerando) Pagerando() bool {
	if pagerando == nil {
		return false
	}

	return Bool(pagerando.Properties.Pagerando)
}

// Is pagerando explicitly set to false?
func (pagerando *pagerando) Disabled() bool {
	return pagerando.Properties.Pagerando != nil && !*pagerando.Properties.Pagerando
}


// Propagate pagerando requirements down from binaries
func pagerandoDepsMutator(mctx android.TopDownMutatorContext) {
	if m, ok := mctx.Module().(*Module); ok && m.pagerando.Pagerando() {
		mctx.WalkDeps(func(dep android.Module, parent android.Module) bool {
			tag := mctx.OtherModuleDependencyTag(dep)
			switch tag {
			case staticDepTag, staticExportDepTag, lateStaticDepTag, wholeStaticDepTag, objDepTag, reuseObjTag:
				if dep, ok := dep.(*Module); ok && dep.pagerando != nil &&
					!dep.pagerando.Disabled() {
					dep.pagerando.Properties.PagerandoDep = true
				}

				// Recursively walk static dependencies
				return true
			}

			// Do not recurse down non-static dependencies
			return false
		})
	}
}

// Create pagerando variants for modules that need them
func pagerandoMutator(mctx android.BottomUpMutatorContext) {
	if c, ok := mctx.Module().(*Module); ok && c.pagerando != nil {
		if c.pagerando.Pagerando() {
			mctx.SetDependencyVariation("pagerando")
			if c.lto == nil || c.lto.Disabled() {
				mctx.ModuleErrorf("does not support LTO")
				return
			}
			c.lto.Properties.Lto.Full = BoolPtr(true)
		} else if c.pagerando.Properties.PagerandoDep ||
			(c.pagerando.Properties.Pagerando == nil &&
			mctx.AConfig().Pagerando()) {
			if c.lto == nil || c.lto.Disabled() {
				// Do not build this module with pagerando since
				// LTO is disabled. This should not be a fatal
				// error.
				c.pagerando.Properties.Pagerando = BoolPtr(false)
				return
			}
			modules := mctx.CreateVariations("", "pagerando")

			modules[0].(*Module).pagerando.Properties.PagerandoDep = false
			modules[0].(*Module).pagerando.Properties.Pagerando = BoolPtr(false)

			modules[1].(*Module).pagerando.Properties.PagerandoDep = false
			modules[1].(*Module).pagerando.Properties.Pagerando = BoolPtr(true)
			modules[1].(*Module).Properties.PreventInstall = true
			modules[1].(*Module).lto.Properties.Lto.Full = BoolPtr(true)

			if c.static() {
				pagerandoStaticLibs := pagerandoStaticLibs(mctx.Config())

				pagerandoStaticLibsMutex.Lock()
				*pagerandoStaticLibs = append(*pagerandoStaticLibs, c.Name())
				pagerandoStaticLibsMutex.Unlock()
			}
		}
	}
}

func pagerandoStaticLibs(config android.Config) *[]string {
	return config.Once("pagerandoStaticLibs", func() interface{} {
		return &[]string{}
	}).(*[]string)
}

func pagerandoMakeVarsProvider(ctx android.MakeVarsContext) {
	pagerandoStaticLibs := pagerandoStaticLibs(ctx.Config())
	sort.Strings(*pagerandoStaticLibs)
	ctx.Strict("SOONG_PAGERANDO_STATIC_LIBRARIES", strings.Join(*pagerandoStaticLibs, " "))
}
